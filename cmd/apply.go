package cmd

import (
	"bytes"
	"fmt"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"log"
	"os"
	"strings"
	"time"

	opConfig "github.com/onepanelio/cli/config"
	"github.com/onepanelio/cli/files"
	"github.com/spf13/cobra"
)

// applyCmd represents the apply command
var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Applies application YAML to your Kubernetes cluster.",
	Run: func(cmd *cobra.Command, args []string) {
		configFilePath := "config.yaml"

		if len(args) > 1 {
			configFilePath = args[0]
			return
		}

		config, err := opConfig.FromFile(configFilePath)
		if err != nil {
			fmt.Printf("Unable to read configuration file: %v", err.Error())
			return
		}

		overlayComponentFirst := "common/application/base"
		baseOverlayComponent := config.GetOverlayComponent(overlayComponentFirst)
		applicationBaseKustomizeTemplate := TemplateFromSimpleOverlayedComponents(baseOverlayComponent)
		applicationResult, err := GenerateKustomizeResult(*config, applicationBaseKustomizeTemplate)
		if err != nil {
			log.Printf("Error generating result %v", err.Error())
			return
		}

		applicationKubernetesYamlFilePath := ".application.kubernetes.yaml"

		existsApp, err := files.Exists(applicationKubernetesYamlFilePath)
		if err != nil {
			log.Printf("Unable to check if file %v exists", applicationKubernetesYamlFilePath)
			return
		}

		var applicationKubernetesFile *os.File = nil
		if !existsApp {
			applicationKubernetesFile, err = os.Create(applicationKubernetesYamlFilePath)
			if err != nil {
				log.Printf("Unable to create file: error %v", err.Error())
				return
			}
		} else {
			applicationKubernetesFile, err = os.OpenFile(applicationKubernetesYamlFilePath, os.O_RDWR|os.O_TRUNC, 0)
			if err != nil {
				log.Printf("Unable to open file: error %v", err.Error())
				return
			}
		}

		if _, err := applicationKubernetesFile.WriteString(applicationResult); err != nil {
			log.Printf("Error writing to temporary file: %v", err.Error())
			return
		}

		fmt.Printf("Starting deployment...\n\n")

		resApp := ""
		errResApp := ""

		resApp, errResApp, err = applyKubernetesFile(applicationKubernetesYamlFilePath)

		log.Printf("%v", resApp)
		if errResApp != "" {
			log.Printf("%v", errResApp)
		}

		if err != nil {
			fmt.Printf("\nFailed: %v", err.Error())
			return
		}
		//Once applied, verify the application is running before moving on with the rest
		//of the yaml.
		applicationRunning := false
		podName := "application-controller-manager-0"
		podNamespace := "application-system"
		podInfoRes := ""
		podInfoErrRes := ""
		var podInfoErr error
		for !applicationRunning {
			podInfoRes, podInfoErrRes, podInfoErr = getPodInfo(podName, podNamespace)
			if podInfoErr != nil {
				fmt.Printf("\nFailed: %v", podInfoErr.Error())
				return
			}
			if podInfoErrRes != "" {
				fmt.Printf("\n: %v", podInfoErrRes)
				return
			}
			if podInfoRes == "" {
				fmt.Printf("\nNo response from first pod check.")
				return
			}

			lines := strings.Split(podInfoRes, "\n")
			if len(lines) > 1 {
				if strings.Contains(lines[1], "Running") {
					applicationRunning = true
				}
			}
		}

		//Apply the rest of the yaml
		kustomizeTemplate := TemplateFromSimpleOverlayedComponents(config.GetOverlayComponents(overlayComponentFirst))

		result, err := GenerateKustomizeResult(*config, kustomizeTemplate)
		if err != nil {
			log.Printf("Error generating result %v", err.Error())
			return
		}

		finalKubernetesYamlFilePath := ".kubernetes.yaml"

		exists, err := files.Exists(finalKubernetesYamlFilePath)
		if err != nil {
			log.Printf("Unable to check if file %v exists", finalKubernetesYamlFilePath)
			return
		}

		var finalKubernetesFile *os.File = nil
		if !exists {
			finalKubernetesFile, err = os.Create(finalKubernetesYamlFilePath)
			if err != nil {
				log.Printf("Unable to create file: error %v", err.Error())
				return
			}
		} else {
			finalKubernetesFile, err = os.OpenFile(finalKubernetesYamlFilePath, os.O_RDWR|os.O_TRUNC, 0)
			if err != nil {
				log.Printf("Unable to open file: error %v", err.Error())
				return
			}
		}

		if _, err := finalKubernetesFile.WriteString(result); err != nil {
			log.Printf("Error writing to temporary file: %v", err.Error())
			return
		}

		res := ""
		errRes := ""

		for i := 0; i < 5; i++ {
			res, errRes, err = applyKubernetesFile(finalKubernetesYamlFilePath)
			if !strings.Contains(errRes, "no matches for kind") {
				break
			}

			fmt.Printf(".")
			fmt.Printf(".")

			time.Sleep(time.Second * 3)
		}

		log.Printf("%v", res)
		if errRes != "" {
			log.Printf("%v", errRes)
		}

		if err != nil {
			fmt.Printf("\nDeployment failed: %v", err.Error())
		} else {
			fmt.Printf("\nDeployment is complete.\n")
		}
	},
}

func init() {
	rootCmd.AddCommand(applyCmd)
}

func getPodInfo(podName string, podNamespace string) (res string, errMessage string, err error) {
	kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()
	kubeConfigFlags.Namespace = &podNamespace
	matchVersionKubeConfigFlags := cmdutil.NewMatchVersionFlags(kubeConfigFlags)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	f := cmdutil.NewFactory(matchVersionKubeConfigFlags)

	ioStreams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    out,
		ErrOut: errOut,
	}
	cmd := get.NewCmdGet("kubectl", f, ioStreams)
	cmd.Run(cmd, []string{"pod", podName})

	res = out.String()
	errMessage = errOut.String()
	return
}

func applyKubernetesFile(filePath string) (res string, errMessage string, err error) {
	kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()
	matchVersionKubeConfigFlags := cmdutil.NewMatchVersionFlags(kubeConfigFlags)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	f := cmdutil.NewFactory(matchVersionKubeConfigFlags)
	ioStreams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    out,
		ErrOut: errOut,
	}
	cmd := apply.NewCmdApply("kubectl", f, ioStreams)
	err = cmd.Flags().Set("filename", filePath)
	if err != nil {
		return "", "", err
	}
	err = cmd.Flags().Set("validate", "false")
	if err != nil {
		return "", "", err
	}
	cmd.Run(cmd, []string{})

	res = out.String()
	errMessage = errOut.String()

	return
}
