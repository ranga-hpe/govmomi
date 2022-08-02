/*
Copyright (c) 2015 VMware, Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vnic

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/vmware/govmomi/govc/cli"
	"github.com/vmware/govmomi/govc/flags"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"github.com/vmware/govmomi/object"
)

type change struct {
	*flags.HostSystemFlag

	mtu int32
	dswitch string
	pg string
}

func init() {
	cli.Register("host.vnic.change", &change{})
}

func (cmd *change) Register(ctx context.Context, f *flag.FlagSet) {
	cmd.HostSystemFlag, ctx = flags.NewHostSystemFlag(ctx)
	cmd.HostSystemFlag.Register(ctx, f)

	f.Var(flags.NewInt32(&cmd.mtu), "mtu", "vmk MTU")
	f.StringVar(&cmd.pg, "dportgroup", "", "vmk target dportgroup name")
	f.StringVar(&cmd.dswitch, "dswitch", "", "vmk target dswitch name")

}

func (cmd *change) Process(ctx context.Context) error {
	if err := cmd.HostSystemFlag.Process(ctx); err != nil {
		return err
	}

	return nil
}

func (cmd *change) Usage() string {
	return "DEVICE"
}

func (cmd *change) Description() string {
	return `Change a virtual nic DEVICE.

Examples:
  govc host.vnic.change -host hostname -mtu 9000 vmk1`
}

func (cmd *change) Run(ctx context.Context, f *flag.FlagSet) error {
	if f.NArg() != 1 {
		return flag.ErrHelp
	}

	device := f.Arg(0)

	ns, err := cmd.HostNetworkSystem()
	if err != nil {
		return err
	}

	var mns mo.HostNetworkSystem

	err = ns.Properties(ctx, ns.Reference(), []string{"networkInfo"}, &mns)
	if err != nil {
		return err
	}

	for _, nic := range mns.NetworkInfo.Vnic {
		if nic.Device == device {
			if cmd.mtu != 0 {
				nic.Spec.Mtu = cmd.mtu
			}
			if cmd.dswitch != "" {
				dvPortsInfo, err := cmd.getPortInfo(ctx)
				if err != nil {
					return err
				}

				if len(dvPortsInfo) == 0 {
					return fmt.Errorf("No ports for portgroup %s\n", cmd.pg)
				}

				dvPort := &types.DistributedVirtualSwitchPortConnection {
					SwitchUuid: dvPortsInfo[0].DvsUuid,
					PortKey:    dvPortsInfo[0].Key,
				}
				nic.Spec.DistributedVirtualPort = dvPort
				nic.Spec.Portgroup = ""
			} else {
				nic.Spec.DistributedVirtualPort = nil
				if cmd.pg != "" {
					nic.Spec.Portgroup = cmd.pg
				}
			}
			return ns.UpdateVirtualNic(ctx, device, nic.Spec)
		}
	}

	return errors.New(device + " not found")
}

func (cmd *change) getPortInfo(ctx context.Context) ([]types.DistributedVirtualPort, error) {

	res := []types.DistributedVirtualPort{}

	finder, err := cmd.Finder()
	if err != nil {
		return res, err
	}

	// Retrieve DVS reference
	net, err := finder.Network(ctx, cmd.dswitch)
	if err != nil {
		return res, err
	}

	// Convert to DVS object type
	dvs, ok := net.(*object.DistributedVirtualSwitch)
	if !ok {
		return res, fmt.Errorf("%s (%s) is not a DVS", cmd.dswitch, net.Reference().Type)
	}

	// Set base search criteria
	criteria := types.DistributedVirtualSwitchPortCriteria{
		// Connected:  types.NewBool(true),
		// Active:     types.NewBool(true),
		UplinkPort: types.NewBool(false),
		Inside:     types.NewBool(true),
		// Connected:  types.NewBool(cmd.connected),
		// Active:     types.NewBool(cmd.active),
		// UplinkPort: types.NewBool(cmd.uplinkPort),
		// Inside:     types.NewBool(cmd.inside),
	}

	// If a distributed virtual portgroup path is set, then add its portgroup key to the base criteria
	if len(cmd.pg) > 0 {
		// Retrieve distributed virtual portgroup reference
		net, err = finder.Network(ctx, cmd.pg)
		if err != nil {
			return res, err
		}

		// Convert distributed virtual portgroup object type
		dvpg, ok := net.(*object.DistributedVirtualPortgroup)
		if !ok {
			return res, fmt.Errorf("%s (%s) is not a DVPG", cmd.pg, net.Reference().Type)
		}

		// Obtain portgroup key property
		var dvp mo.DistributedVirtualPortgroup
		if err := dvpg.Properties(ctx, dvpg.Reference(), []string{"key"}, &dvp); err != nil {
			return res, err
		}

		// Add portgroup key to port search criteria
		criteria.PortgroupKey = []string{dvp.Key}
	}

	res, err = dvs.FetchDVPorts(ctx, &criteria)
	if err != nil {
		return res, err
	}

	return res, err
}
