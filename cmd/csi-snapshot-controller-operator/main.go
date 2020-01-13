package main

import (
	goflag "flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/huffmanca/cluster-csi-snapshot-controller-operator/pkg/operator"
	"github.com/huffmanca/cluster-csi-snapshot-controller-operator/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"

	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	command := NewCSISnapshotControllerOperatorCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func NewCSISnapshotControllerOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "csi-snapshot-controller-operator",
		Short: "OpenShift CSI Snapshot operator",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}

	cmd.AddCommand(controllercmd.NewControllerCommandConfig("cluster", version.Get(), operator.RunOperator).NewCommand())

	return cmd
}
