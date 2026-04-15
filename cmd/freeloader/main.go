package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "freeloader",
		Short: "Cloud Storage Freeloader — unified free-tier cloud storage",
	}

	root.AddCommand(newMountCmd())
	root.AddCommand(newProviderCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newRepairCmd())

	return root
}

func newMountCmd() *cobra.Command {
	var mountPath string
	var configPath string

	cmd := &cobra.Command{
		Use:   "mount",
		Short: "Mount the virtual drive",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath != "" {
				cmd.Printf("Using config: %s\n", configPath)
			}
			cmd.Printf("Mounting at %s\n", mountPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&mountPath, "path", "", "Mount point path")
	cmd.Flags().StringVar(&configPath, "config", "config.local.yaml", "Config file path")
	return cmd
}

func newProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage storage providers",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add",
		Short: "Add a new provider",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Printf("Adding provider: %s\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("Configured providers:")
			cmd.Println("  (none configured)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove",
		Short: "Remove a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Printf("Removing provider: %s\n", args[0])
			return nil
		},
	})

	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show system health",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("System status: OK")
			return nil
		},
	}
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Force metadata sync",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("Syncing metadata...")
			return nil
		},
	}
}

func newRepairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Check and repair degraded segments",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("Checking segment integrity...")
			return nil
		},
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
