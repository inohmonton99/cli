package cmd

import (
	"fmt"
	"github.com/onepanelio/cli/files"
	"github.com/onepanelio/cli/util"
	"github.com/spf13/cobra"
	"path/filepath"
)

var (
	// skipConfirmDelete if true, will skip the confirmation prompt of the delete command
	skipConfirmDelete bool
)

var deleteCmd = &cobra.Command{
	Use:     "delete",
	Short:   "Deletes onepanel cluster resources",
	Long:    "Delete all onepanel kubernetes cluster resources. Does not delete database unless it is in-cluster.",
	Example: "delete",
	Run: func(cmd *cobra.Command, args []string) {
		if skipConfirmDelete == false {
			fmt.Print("Are you sure you want to delete onepanel? ('y' or 'yes' to confirm. Anything else to cancel): ")
			userInput := ""
			if _, err := fmt.Scanln(&userInput); err != nil {
				fmt.Printf("Unable to get response\n")
				return
			}

			if userInput != "y" && userInput != "yes" {
				return
			}
		}

		filesToDelete := []string{
			filepath.Join(".onepanel", "kubernetes.yaml"),
			filepath.Join(".onepanel", "application.kubernetes.yaml"),
		}

		for _, filePath := range filesToDelete {
			exists, err := files.Exists(filePath)
			if err != nil {
				fmt.Printf("Error checking if onepanel files exist: %v\n", err.Error())
				return
			}

			if !exists {
				fmt.Printf("'%v' file does not exist. Are you in the directory where you run 'opctl init'?\n", filePath)
				return
			}
		}

		fmt.Printf("Deleting onepanel from your cluster...\n")
		for _, filePath := range filesToDelete {
			if err := util.KubectlDelete(filePath); err != nil {
				fmt.Printf("Unable to delete: %v\n", err.Error())
				return
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
	deleteCmd.Flags().BoolVarP(&skipConfirmDelete, "yes", "y", false, "Add this in to skip the confirmation prompt")
}
