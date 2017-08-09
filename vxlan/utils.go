package vxlan

import (
	"net"

	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/vishvananda/netlink"
)

func getNetLink(intfName string) (netlink.Link, error) {
	link, err := netlink.LinkByName(intfName)
	if err != nil {
		return nil, err
	}
	return link, nil
}

func getVxlanNetworks(networks []metadata.Network, host metadata.Host) []metadata.Network {
	vxlanNetworks := []metadata.Network{}
	for _, aNetwork := range networks {
		if _, ok := aNetwork.Metadata["cniConfig"].(map[string]interface{}); !ok {
			continue
		}
		vxlanNetworks = append(vxlanNetworks, aNetwork)
	}

	return vxlanNetworks
}

func getBridgeSubnets(vxlanNetworks []metadata.Network) ([]*net.IPNet, error) {
	var e error
	ipnets := []*net.IPNet{}
	for _, network := range vxlanNetworks {
		var bridgeSubnet string
		conf, _ := network.Metadata["cniConfig"].(map[string]interface{})
		for _, file := range conf {
			props, _ := file.(map[string]interface{})
			bridgeSubnet, _ = props["bridgeSubnet"].(string)
		}
		_, ipnet, err := net.ParseCIDR(bridgeSubnet)
		if err != nil {
			e = errors.Wrap(e, err.Error())
		} else {
			ipnets = append(ipnets, ipnet)
		}
	}
	return ipnets, e
}
