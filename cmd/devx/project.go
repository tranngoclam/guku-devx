package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue/errors"
	"devopzilla.com/guku/internal/project"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage a DevX project",
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := project.Init(context.TODO(), configDir, ""); err != nil {
			return err
		}
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update/Install project dependencies",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := project.Update(configDir); err != nil {
			return err
		}
		return nil
	},
}

var validateCmd = &cobra.Command{
	Use:     "validate",
	Aliases: []string{"v"},
	Short:   "Validate configurations",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := project.Validate(configDir, stackPath); err != nil {
			return fmt.Errorf(errors.Details(err, nil))
		}
		return nil
	},
}

var discoverCmd = &cobra.Command{
	Use:     "discover",
	Aliases: []string{"d"},
	Short:   "Discover traits",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := project.Discover(configDir, showDefs, showTransformers); err != nil {
			return err
		}
		return nil
	},
}

var genCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate bare config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := project.Generate(configDir); err != nil {
			return err
		}
		return nil
	},
}
