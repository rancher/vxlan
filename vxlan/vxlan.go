// Package vxlan provides the capabilities to create a VXLAN overlay network
package vxlan

import (
	"fmt"
	"net"
	"sync"

	"github.com/leodotcloud/log"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/vishvananda/netlink"
)

const (
	changeCheckInterval = 5
	metadataURL         = "http://%s/2016-07-29"
	vxlanMACRange       = "0E:00:00:00:00:00"
	vxlanPort           = 4789
)

const (
	DefaultBridgeName         = "docker0"
	DefaultVxlanMTU           = 1500
	DefaultVxlanVNI           = 1042
	DefaultVxlanInterfaceName = "vtep1042"
	DefaultMetadataAddress    = "169.254.169.250"
)

// Vxlan is used to store the VXLAN overlay information
type Vxlan struct {
	mu sync.Mutex
	m  metadata.Client
	v  *vxlanIntfInfo

	BridgeName         string
	VxlanMTU           int
	VxlanVNI           int
	VxlanInterfaceName string
}

// NewVxlan is used to create a new VXLAN Overlay network
func NewVxlan(metadataAddress string) (*Vxlan, error) {
	m, err := metadata.NewClientAndWait(fmt.Sprintf(metadataURL, metadataAddress))
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create metadata client")
	}

	o := &Vxlan{
		m: m,
	}

	return o, nil
}

// Start is used to start the vxlan overlay
func (o *Vxlan) Start() error {
	log.Infof("vxlan: Start")
	err := o.configure()
	if err != nil {
		return errors.Wrap(err, "Failed to start vxlan")
	}
	go o.m.OnChange(changeCheckInterval, o.onChangeNoError)

	return nil
}

func (o *Vxlan) onChangeNoError(version string) {
	if err := o.Reload(); err != nil {
		log.Errorf("Failed to apply vxlan rules: %v", err)
	}
}

func (o *Vxlan) Reload() error {
	log.Debugf("vxlan: Reload")
	err := o.configure()
	if err != nil {
		err = errors.Wrap(err, "Failed to reload vxlan")
	}
	return err
}

func (o *Vxlan) configure() error {
	log.Debugf("vxlan: configure")
	o.mu.Lock()
	defer o.mu.Unlock()

	// First create the local VTEP interface
	err := o.checkAndCreateVTEP()
	if err != nil {
		return errors.Wrap(err, "Error creating VTEP interface")
	}

	var (
		arpMap        = make(map[string]net.HardwareAddr) // {ContainerIP: mac}
		fdbMap        = make(map[string]net.HardwareAddr) // {HostIP: mac}
		peersHostMap  = make(map[string]string)           // {HostUUID: peerContainerIP}
		peersNetworks = make(map[string]bool)             // {NetwokrUUID: bool}
	)

	allContainers, err := o.m.GetContainers()
	if err != nil {
		return errors.Wrap(err, "Failed to get containers from metadata")
	}
	networks, err := o.m.GetNetworks()
	if err != nil {
		return errors.Wrap(err, "Failed to get networks from metadata")
	}
	hosts, err := o.m.GetHosts()
	if err != nil {
		return errors.Wrap(err, "Failed to get hosts from metadata")
	}
	selfHost, err := o.m.GetSelfHost()
	if err != nil {
		return errors.Wrap(err, "Failed to get self host from metadata")
	}

	vxlanNetworks := getVxlanNetworks(networks, selfHost)

	for _, n := range vxlanNetworks {
		peersNetworks[n.UUID] = true
	}

	for _, h := range hosts {
		if h.UUID == selfHost.UUID {
			continue
		}
		ip := net.ParseIP(h.AgentIP)
		peerMAC, err := getMACAddressForVxlanIP(vxlanMACRange, ip)
		if err != nil {
			log.Errorf("Failed to ParseMAC in peersHosts: %v", err)
			continue
		}

		fdbMap[h.AgentIP] = peerMAC

		peersHostMap[h.UUID] = ip.To4().String()
	}

	for _, c := range allContainers {
		// check if the container networkUUID is part of peersNetworks
		_, isPresentInPeersNetworks := peersNetworks[c.NetworkUUID]

		if !isPresentInPeersNetworks ||
			c.PrimaryIp == "" ||
			c.NetworkFromContainerUUID != "" ||
			c.HostUUID == selfHost.UUID ||
			!(c.State == "running" || c.State == "starting") {
			continue
		}

		peerIPAddress, ok := peersHostMap[c.HostUUID]
		if !ok || c.PrimaryIp == peerIPAddress {
			// skip peer containers
			continue
		}
		peerIP := net.ParseIP(peersHostMap[c.HostUUID])
		peerMAC, err := getMACAddressForVxlanIP(vxlanMACRange, peerIP)
		if err != nil {
			log.Errorf("Failed to ParseMAC in nonPeersContainers: %v", err)
			continue
		}

		arpMap[c.PrimaryIp] = peerMAC
	}

	bridgeSubnets, err := getBridgeSubnets(vxlanNetworks)
	if err != nil {
		return err
	}

	vtepLink, err := getNetLink(o.v.name)
	if err != nil {
		return err
	}
	currentARPEntries, err := getCurrentARPEntries(vtepLink, bridgeSubnets)
	if err != nil {
		return err
	}
	err = updateARP(currentARPEntries, getDesiredARPEntries(vtepLink, arpMap))
	if err != nil {
		return err
	}
	currentFDBEntries, err := getCurrentFDBEntries(vtepLink)
	if err != nil {
		return err
	}
	err = updateFDB(currentFDBEntries, getDesiredFDBEntries(vtepLink, fdbMap))
	if err != nil {
		return err
	}

	return nil
}

// getMyVTEPInfo is used to figure out the MAC address to be assigned
// for the VTEP address.
func (o *Vxlan) getMyVTEPInfo() (net.HardwareAddr, error) {
	log.Debugf("vxlan: GetMyVTEPInfo")

	selfHost, err := o.m.GetSelfHost()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get self host from metadata")
	}

	myRancherIPString := selfHost.AgentIP
	myRancherIP := net.ParseIP(myRancherIPString)
	mac, err := getMACAddressForVxlanIP(vxlanMACRange, myRancherIP)
	if err != nil {
		return nil, err
	}

	log.Debugf("vxlan: my vtep info mac:%v", mac)
	return mac, nil
}

func (o *Vxlan) SetDefaultVxlanInterfaceInfo() error {
	log.Debugf("vxlan: SetDefaultVxlanInterfaceInfo")
	mac, err := o.getMyVTEPInfo()
	if err != nil {
		return err
	}

	o.v = &vxlanIntfInfo{
		name: o.VxlanInterfaceName,
		vni:  o.VxlanVNI,
		port: vxlanPort,
		mac:  mac,
		mtu:  o.VxlanMTU,
	}

	return nil
}

// createVTEP creates a vxlan interface with the default values
func (o *Vxlan) createVTEP() error {
	log.Debugf("vxlan: trying to create vtep: %v", o.v)
	err := createVxlanInterface(o.v)
	if err != nil {
		// The errors are really mysterious, hence
		// documenting the ones I came across.
		// invalid argument:
		//   Could mean there is another interface with similar properties.
		log.Errorf("Error creating vxlan interface v=%v: err=%v", o.v, err)
		return err
	}

	log.Infof("vxlan: successfully created interface %v", o.v)
	return nil
}

func (o *Vxlan) checkAndCreateVTEP() error {
	log.Debugf("vxlan: checkAndCreateVTEP")

	l, err := findVxlanInterface(o.v.name)
	if err != nil {
		return o.createVTEP()
	}

	if l == nil {
		return errors.New("Couldn't find link and didn't get error")
	}

	if l.Attrs().MasterIndex <= 0 {
		bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: o.BridgeName}}
		err = netlink.LinkSetMaster(l, bridge)
		if err != nil {
			return err
		}
	}
	return nil
}
