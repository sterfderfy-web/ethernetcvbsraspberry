//go:build !puregohid
// +build !puregohid

package main

import (
	"fmt"
	"errors"

	"github.com/BertoldVdb/ms-tools/gohid"
	"github.com/sstallion/go-hid"
)

func SearchDevice(foundHandler func(info *hid.DeviceInfo) error) error {
	for _, vid := range []uint16{uint16(CLI.VID), uint16(CLI.VID2)} {
		if err := hid.Enumerate(vid, uint16(CLI.PID), func(info *hid.DeviceInfo) error {
			if CLI.Serial != "" && info.SerialNbr != CLI.Serial {
				return nil
			}
			if CLI.RawPath != "" && info.Path != CLI.RawPath {
				return nil
			}

			return foundHandler(info)
		}); err != nil {
			return err
		}
	}
	return nil
}

func OpenDevice() (gohid.HIDDevice, error) {
	var matches []*hid.DeviceInfo
	err := SearchDevice(func(info *hid.DeviceInfo) error {
		copyInfo := *info
		matches = append(matches, &copyInfo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if CLI.RawPath == "" {
		return nil, errors.New("diagnostic build requires --raw-path")
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("expected exactly one HID path, found %d", len(matches))
	}
	return hid.OpenPath(matches[0].Path)
}

type ListHIDCmd struct {
}

func (l *ListHIDCmd) Run(c *Context) error {
	return SearchDevice(func(info *hid.DeviceInfo) error {
		fmt.Printf("%s: ID %04x:%04x %s %s\n",
			info.Path, info.VendorID, info.ProductID, info.MfrStr, info.ProductStr)
		fmt.Println("Device Information:")
		fmt.Printf("\tPath         %s\n", info.Path)
		fmt.Printf("\tVendorID     %04x\n", info.VendorID)
		fmt.Printf("\tProductID    %04x\n", info.ProductID)
		fmt.Printf("\tSerialNbr    %s\n", info.SerialNbr)
		fmt.Printf("\tReleaseNbr   %x.%x\n", info.ReleaseNbr>>8, info.ReleaseNbr&0xff)
		fmt.Printf("\tMfrStr       %s\n", info.MfrStr)
		fmt.Printf("\tProductStr   %s\n", info.ProductStr)
		fmt.Printf("\tUsagePage    %#x\n", info.UsagePage)
		fmt.Printf("\tUsage        %#x\n", info.Usage)
		fmt.Printf("\tInterfaceNbr %d\n", info.InterfaceNbr)
		fmt.Println()

		return nil
	})
	return nil
}
