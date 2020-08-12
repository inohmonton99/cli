package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/onepanelio/cli/config"
	"github.com/onepanelio/cli/files"
	"github.com/onepanelio/cli/manifest"
	"github.com/onepanelio/cli/template"
	"github.com/onepanelio/cli/util"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

const (
	manifestsFilePath             = ".onepanel/manifests"
	artifactRepositoryProviderS3  = "s3"
	artifactRepositoryProviderGcs = "gcs"
)

var (
	ConfigurationFilePath      string
	ParametersFilePath         string
	Provider                   string
	DNS                        string
	ArtifactRepositoryProvider string
	Dev                        bool
	EnableEFKLogging           bool
	EnableHTTPS                bool
	EnableCertManager          bool
	EnableMetalLb              bool
	GPUDevicePlugins           []string
	Services                   []string
)

type ProviderProperties struct {
	IsCloud bool
}

var providerProperties = map[string]ProviderProperties{
	"minikube": {
		IsCloud: false,
	},
	"microk8s": {
		IsCloud: false,
	},
	"gke": {
		IsCloud: true,
	},
	"eks": {
		IsCloud: true,
	},
	"aks": {
		IsCloud: true,
	},
}

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Gets latest manifests and generates params.yaml file.",
	Run: func(cmd *cobra.Command, args []string) {

		if err := validateInput(); err != nil {
			log.Printf(err.Error())
			return
		}

		log.Printf("Initializing...")
		configFile := filepath.Join(".onepanel/cli_config.yaml")
		exists, err := files.Exists(configFile)
		if err != nil {
			log.Printf("[error] checking for config file %v", configFile)
			return
		}

		if !exists {
			if err := manifest.CreateGithubSourceConfigFile(configFile); err != nil {
				log.Printf("[error] creating default source config: %v", err.Error())
				return
			}
		}

		source, err := manifest.LoadManifestSourceFromFileConfig(configFile)
		if err != nil {
			log.Printf("[error] loading manifest source: %v", err.Error())
			return
		}

		// When updating cli versions, the cli_config.yaml may already exist.
		// Check if we need to generate a new cli_config.yaml, to match the cli version.
		tag := config.ManifestsRepositoryTag
		if source.GetSourceType() == manifest.SourceGithub {
			if source.GetTag() != "" {
				if tag != source.GetTag() {
					if err := manifest.CreateGithubSourceConfigFile(configFile); err != nil {
						log.Printf("[error] creating default source config: %v", err.Error())
						return
					}
					source, err = manifest.LoadManifestSourceFromFileConfig(configFile)
					if err != nil {
						log.Printf("[error] loading manifest source: %v", err.Error())
						return
					}
				}
			}
		} else {
			fmt.Printf("cli_config.yaml is using %v as source, ignoring CLI tag: %v", manifest.SourceDirectory, config.CLIVersion)
		}

		if err := source.MoveToDirectory(filepath.Join(manifestsFilePath)); err != nil {
			log.Printf("[error] %v", err.Error())
			return
		}

		manifestsRepoPath, err := source.GetManifestPath()
		if err != nil {
			log.Printf("[error] %v", err.Error())
			return
		}

		if err := files.CreateIfNotExist(ParametersFilePath); err != nil {
			log.Println(err.Error())
		}

		setup := config.Config{
			ApiVersion: "opdef.apps.onepanel.io/v1alpha1",
			Kind:       "OpDef",
			Spec: config.ConfigSpec{
				Components:    []string{},
				ManifestsRepo: manifestsRepoPath,
				Params:        ParametersFilePath,
			},
		}

		loadedManifest, err := manifest.LoadManifest(manifestsRepoPath)
		if err != nil {
			log.Printf("[error] LoadManifest %v", err.Error())
		}

		bld := manifest.CreateBuilder(loadedManifest)
		if err := bld.AddCommonComponents(); err != nil {
			log.Printf("[error] AddCommonComponents %v", err.Error())
			return
		}

		if err := addCloudProviderToManifestBuilder(Provider, bld); err != nil {
			log.Printf("[error] Adding Cloud Provider: %v", err.Error())
			return
		}

		if err := addDNSProviderToManifestBuilder(DNS, bld); err != nil {
			log.Printf("[error] Adding Dns Provider: %v", err.Error())
			return
		}

		if EnableEFKLogging {
			if err := bld.AddComponent("logging"); err != nil {
				log.Printf("[error] Adding logging component: %v", err.Error())
				return
			}
		}

		if GPUDevicePlugins != nil {
			if err := bld.AddComponent("gpu-plugins"); err != nil {
				log.Printf("[error] Adding GPU plugins component: %v", err.Error())
				return
			}

			bld.AddOverlayContender(GPUDevicePlugins...)
		}

		bld.AddOverlayContender("cloud")

		if EnableHTTPS {
			bld.AddOverlayContender("https")
		}

		if Services != nil {
			if err := bld.AddComponent(Services...); err != nil {
				log.Printf("[error] Adding Components: %v", err.Error())
			}
		}

		if err := bld.Build(); err != nil {
			log.Printf("[error] building components and overlays: %v", err.Error())
			return
		}

		for _, overlayComponent := range bld.GetOverlayComponents() {
			setup.AddComponent(overlayComponent.Component().PathWithBase())
			for _, overlay := range overlayComponent.Overlays() {
				setup.AddOverlay(overlay.Path())
			}
		}

		builder := template.NewBuilderFromConfig(setup)
		if err := builder.Build(); err != nil {
			log.Printf("err generating config. Error %v", err.Error())
			return
		}

		mergedParams, err := util.LoadDynamicYamlFromFile(ParametersFilePath)
		if err != nil {
			log.Printf("[error] loading params file: %v", err.Error())
			return
		}

		mergedParams.Merge(bld.GetYamls()...)

		mergedParams.Put("application.insecure", !EnableHTTPS)
		mergedParams.Put("application.provider", Provider)

		removeUneededArtifactRepositoryProviders(mergedParams)

		mergedParams.Sort()
		paramsString, err := mergedParams.String()
		if err != nil {
			log.Printf("[error] unable to write params to a string")
			return
		}

		paramsFile, err := os.OpenFile(ParametersFilePath, os.O_RDWR|os.O_TRUNC, 0)
		if err != nil {
			log.Printf("Error opening parameters file: %v", err.Error())
			return
		}

		if _, err := paramsFile.WriteString(paramsString); err != nil {
			log.Printf("Error writing merged parameters: %v", err.Error())
			return
		}

		file, err := os.Create(ConfigurationFilePath)
		if err != nil {
			log.Printf("unable to create %v file: %v", ConfigurationFilePath, err.Error())
			return
		}

		setupData, err := yaml.Marshal(setup)
		if err != nil {
			log.Printf("unable to marshal yaml data: %v", err.Error())
			return
		}

		if _, err := file.Write(setupData); err != nil {
			log.Printf("unable to write yaml data: %v", err.Error())
			return
		}

		fmt.Printf("Configuration has been created with\n")
		fmt.Printf("- Provider: %v\n", Provider)

		if DNS != "" {
			fmt.Printf("- Dns: %v\n", DNS)
		}

		fmt.Printf("- Configuration file: %v\n", ConfigurationFilePath)
		fmt.Printf("- Parameters file has been created with placeholders: %v\n", ParametersFilePath)
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVarP(&Provider, "provider", "p", "", "Cloud provider. Valid values are: aks, gke, eks")
	initCmd.Flags().StringVarP(&DNS, "dns-provider", "d", "", "Provider for DNS. Valid values are: azuredns, clouddns (google), cloudflare, route53")
	initCmd.Flags().StringVarP(&ArtifactRepositoryProvider, "artifact-repository-provider", "", "", "Artifact Storage Provider for argo. Valid values are: s3, gcs")
	initCmd.Flags().StringVarP(&ConfigurationFilePath, "config", "c", "config.yaml", "File path of the resulting config file")
	initCmd.Flags().StringVarP(&ParametersFilePath, "params", "e", "params.yaml", "File path of the resulting parameters file")
	initCmd.Flags().BoolVarP(&EnableEFKLogging, "enable-efk-logging", "", false, "Enable Elasticsearch, Fluentd and Kibana (EFK) logging")
	initCmd.Flags().BoolVarP(&EnableHTTPS, "enable-https", "", false, "Enable HTTPS scheme and redirect all requests to https://")
	initCmd.Flags().BoolVarP(&EnableCertManager, "enable-cert-manager", "", false, "Automatically create/renew TLS certs using Let's Encrypt")
	initCmd.Flags().BoolVarP(&EnableMetalLb, "enable-metallb", "", false, "Automatically create a LoadBalancer for non-cloud deployments.")
	initCmd.Flags().StringSliceVarP(&GPUDevicePlugins, "gpu-device-plugins", "", nil, "Install NVIDIA and/or AMD gpu device plugins. Valid values can be comma separated and are: amd, nvidia")
	initCmd.Flags().StringSliceVarP(&Services, "services", "", nil, "Install additional services. Valid values can be comma separated and are: modeldb")

	initCmd.MarkFlagRequired("provider")
}

func validateInput() error {
	if EnableCertManager && !EnableHTTPS {
		return fmt.Errorf("enable-https flag is required when enable-cert-manager is set")
	}

	if EnableCertManager && DNS == "" {
		return fmt.Errorf("dns-provider flag is required when enable-cert-manager is set")
	}

	if !EnableCertManager && DNS != "" {
		return fmt.Errorf("enable-cert-manager flag is required when dns-provider is set")
	}

	if err := validateProvider(Provider); err != nil {
		return err
	}

	if err := validateDNS(DNS); err != nil {
		return err
	}

	if err := validateGPUPlugins(GPUDevicePlugins); err != nil {
		return err
	}

	err := validateServices(Services)

	return err
}

func validateProvider(prov string) error {
	_, ok := providerProperties[prov]
	if !ok {
		return fmt.Errorf("Unsupported provider %v", prov)
	}

	return nil
}

func validateArtifactRepositoryProvider(arRepoProv string) error {
	if arRepoProv == "" {
		return errors.New("artifact repository provider cannot be empty")
	}

	if arRepoProv == artifactRepositoryProviderS3 ||
		arRepoProv == artifactRepositoryProviderGcs {
		return nil
	}
	return errors.New("unrecognized artifact repository provider value")
}

func validateDNS(dns string) error {
	if dns != "route53" && dns != "" && dns != "clouddns" && dns != "azuredns" && dns != "cloudflare" {
		return fmt.Errorf("unsupported dns %v", dns)
	}

	return nil
}

func validateGPUPlugins(gpuPlugins []string) error {
	if gpuPlugins == nil {
		return nil
	}

	for _, p := range GPUDevicePlugins {
		if p != "amd" && p != "nvidia" {
			return fmt.Errorf("%v is not a valid --gpu-device-plugins value", p)
		}
	}

	return nil
}

func validateServices(services []string) error {
	if services == nil {
		return nil
	}

	for _, c := range services {
		if c != "modeldb" {
			return fmt.Errorf("%v is not a valid --component value", c)
		}
	}

	return nil
}

func addCloudProviderToManifestBuilder(provider string, builder *manifest.Builder) error {
	builder.AddOverlayContender(provider)

	if (provider != "minikube" && provider != "microk8s") && EnableCertManager {
		if err := builder.AddComponent("cert-manager"); err != nil {
			return err
		}
	}

	if (provider == "minikube" || provider == "microk8s") && EnableMetalLb {
		if err := builder.AddComponent("metallb"); err != nil {
			return err
		}
	}

	if err := builder.AddComponent("storage"); err != nil {
		return err
	}
	return nil
}

func addDNSProviderToManifestBuilder(dns string, builder *manifest.Builder) error {
	if dns == "" {
		return nil
	}

	builder.AddOverlayContender(dns)

	overlay := strings.Join([]string{"cert-manager", "overlays", dns}, string(os.PathSeparator))
	return builder.AddOverlay(overlay)
}

func removeUneededArtifactRepositoryProviders(mergedParams *util.DynamicYaml) {
	artifactRepoProviders := []string{artifactRepositoryProviderS3, artifactRepositoryProviderGcs}
	var nodeKeyStr string
	for _, artRepoProv := range artifactRepoProviders {
		if ArtifactRepositoryProvider == artRepoProv {
			continue
		}
		nodeKeyStr = "artifactRepository." + artRepoProv
		nodeKey, _ := mergedParams.Get(nodeKeyStr)
		if nodeKey != nil {
			err := mergedParams.Delete(nodeKeyStr)
			if err != nil {
				log.Printf("error during init, artifact repository provider. %v", err.Error())
				return
			}
		}
	}

	parentValue := mergedParams.GetValue("artifactRepository")
	if len(parentValue.Content) == 0 {
		if err := mergedParams.Delete("artifactRepository"); err != nil {
			log.Printf("error during init, artifact repository provider. %v", err)
		}
	}
}
