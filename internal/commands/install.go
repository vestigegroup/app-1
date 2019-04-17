package commands

import (
	"fmt"
	"os"

	"github.com/deislabs/duffle/pkg/action"
	"github.com/deislabs/duffle/pkg/claim"
	"github.com/deislabs/duffle/pkg/credentials"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type installOptions struct {
	parametersOptions
	credentialOptions
	registryOptions
	pullOptions
	orchestrator  string
	kubeNamespace string
	stackName     string
}

type nameKind uint

const (
	_ nameKind = iota
	nameKindEmpty
	nameKindFile
	nameKindDir
	nameKindReference
)

const longDescription = `Install an application.
By default, the application definition in the current directory will be
installed. The APP_NAME can also be:
- a path to a Docker Application definition (.dockerapp) or a CNAB bundle.json
- a registry Application Package reference`

const example = `$ docker app install myapp.dockerapp --name myinstallation --target-context=mycontext
$ docker app install myrepo/myapp:mytag --name myinstallation --target-context=mycontext
$ docker app install bundle.json --name myinstallation --credential-set=mycredentials.yml`

func installCmd(dockerCli command.Cli) *cobra.Command {
	var opts installOptions

	cmd := &cobra.Command{
		Use:     "install [APP_NAME] [--name INSTALLATION_NAME] [--target-context TARGET_CONTEXT] [OPTIONS]",
		Aliases: []string{"deploy"},
		Short:   "Install an application",
		Long:    longDescription,
		Example: example,
		Args:    cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(dockerCli, firstOrEmpty(args), opts)
		},
	}
	opts.parametersOptions.addFlags(cmd.Flags())
	opts.credentialOptions.addFlags(cmd.Flags())
	opts.registryOptions.addFlags(cmd.Flags())
	opts.pullOptions.addFlags(cmd.Flags())
	cmd.Flags().StringVar(&opts.orchestrator, "orchestrator", "", "Orchestrator to install on (swarm, kubernetes)")
	cmd.Flags().StringVar(&opts.kubeNamespace, "kubernetes-namespace", "default", "Kubernetes namespace to install into")
	cmd.Flags().StringVar(&opts.stackName, "name", "", "Installation name (defaults to application name)")

	return cmd
}

func runInstall(dockerCli command.Cli, appname string, opts installOptions) error {
	defer muteDockerCli(dockerCli)()
	opts.SetDefaultTargetContext(dockerCli)

	bind, err := requiredBindMount(opts.targetContext, opts.orchestrator, dockerCli.ContextStore())
	if err != nil {
		return err
	}
	bundleStore, installationStore, credentialStore, err := prepareStores(opts.targetContext)
	if err != nil {
		return err
	}

	bndl, err := resolveBundle(dockerCli, bundleStore, appname, opts.pull, opts.insecureRegistries)
	if err != nil {
		return err
	}
	if err := bndl.Validate(); err != nil {
		return err
	}
	installationName := opts.stackName
	if installationName == "" {
		installationName = bndl.Name
	}
	if installation, err := installationStore.Read(installationName); err == nil {
		// A failed installation can be overridden, but with a warning
		if isInstallationFailed(&installation) {
			fmt.Fprintf(os.Stderr, "WARNING: installing over previously failed installation %q\n", installationName)
		} else {
			// Return an error in case of successful installation, or even failed upgrade, which means
			// their was already a successful installation.
			return fmt.Errorf("Installation %q already exists, use 'docker app upgrade' instead", installationName)
		}
	}
	c, err := claim.New(installationName)
	if err != nil {
		return err
	}

	driverImpl, errBuf, err := prepareDriver(dockerCli, bind, nil)
	if err != nil {
		return err
	}
	c.Bundle = bndl

	if err := mergeBundleParameters(c,
		withFileParameters(opts.parametersFiles),
		withCommandLineParameters(opts.overrides),
		withOrchestratorParameters(opts.orchestrator, opts.kubeNamespace),
		withSendRegistryAuth(opts.sendRegistryAuth),
	); err != nil {
		return err
	}
	creds, err := prepareCredentialSet(bndl, opts.CredentialSetOpts(dockerCli, credentialStore)...)
	if err != nil {
		return err
	}
	if err := credentials.Validate(creds, bndl.Credentials); err != nil {
		return err
	}

	inst := &action.Install{
		Driver: driverImpl,
	}
	err = inst.Run(c, creds, dockerCli.Out())
	// Even if the installation failed, the installation is persisted with its failure status,
	// so any installation needs a clean uninstallation.
	err2 := installationStore.Store(*c)
	if err != nil {
		return fmt.Errorf("install failed: %s", errBuf)
	}
	return err2
}
