package main

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/cloudfoundry/bosh-bootloader/application"
	"github.com/cloudfoundry/bosh-bootloader/aws"
	"github.com/cloudfoundry/bosh-bootloader/aws/clientmanager"
	"github.com/cloudfoundry/bosh-bootloader/aws/cloudformation"
	"github.com/cloudfoundry/bosh-bootloader/aws/cloudformation/templates"
	"github.com/cloudfoundry/bosh-bootloader/aws/ec2"
	"github.com/cloudfoundry/bosh-bootloader/aws/iam"
	"github.com/cloudfoundry/bosh-bootloader/bbl/constants"
	"github.com/cloudfoundry/bosh-bootloader/bosh"
	"github.com/cloudfoundry/bosh-bootloader/boshinit"
	"github.com/cloudfoundry/bosh-bootloader/boshinit/manifests"
	"github.com/cloudfoundry/bosh-bootloader/commands"
	"github.com/cloudfoundry/bosh-bootloader/helpers"
	"github.com/cloudfoundry/bosh-bootloader/ssl"
	"github.com/cloudfoundry/bosh-bootloader/storage"
	"github.com/square/certstrap/pkix"
)

var (
	Version string
)

func main() {
	// Command Set
	commandSet := application.CommandSet{
		commands.HelpCommand:             nil,
		commands.VersionCommand:          nil,
		commands.UpCommand:               nil,
		commands.DestroyCommand:          nil,
		commands.DirectorAddressCommand:  nil,
		commands.DirectorUsernameCommand: nil,
		commands.DirectorPasswordCommand: nil,
		commands.DirectorCACertCommand:   nil,
		commands.BOSHCACertCommand:       nil,
		commands.SSHKeyCommand:           nil,
		commands.CreateLBsCommand:        nil,
		commands.UpdateLBsCommand:        nil,
		commands.DeleteLBsCommand:        nil,
		commands.LBsCommand:              nil,
		commands.EnvIDCommand:            nil,
	}

	// Utilities
	uuidGenerator := helpers.NewUUIDGenerator(rand.Reader)
	stringGenerator := helpers.NewStringGenerator(rand.Reader)
	envIDGenerator := helpers.NewEnvIDGenerator(rand.Reader)
	logger := application.NewLogger(os.Stdout)
	stderrLogger := application.NewLogger(os.Stderr)
	sslKeyPairGenerator := ssl.NewKeyPairGenerator(rsa.GenerateKey, pkix.CreateCertificateAuthority, pkix.CreateCertificateSigningRequest, pkix.CreateCertificateHost)

	// Usage Command
	usage := commands.NewUsage(os.Stdout)
	storage.GetStateLogger = stderrLogger

	commandLineParser := application.NewCommandLineParser(usage.Print, commandSet)
	configurationParser := application.NewConfigurationParser(commandLineParser)
	configuration, err := configurationParser.Parse(os.Args[1:])
	if err != nil {
		fail(err)
	}

	stateStore := storage.NewStore(configuration.Global.StateDir)
	stateValidator := application.NewStateValidator(configuration.Global.StateDir)

	// Amazon
	awsConfiguration := aws.Config{
		AccessKeyID:      configuration.State.AWS.AccessKeyID,
		SecretAccessKey:  configuration.State.AWS.SecretAccessKey,
		Region:           configuration.State.AWS.Region,
		EndpointOverride: configuration.Global.EndpointOverride,
	}

	clientProvider := &clientmanager.ClientProvider{EndpointOverride: configuration.Global.EndpointOverride}
	clientProvider.SetConfig(awsConfiguration)

	awsCredentialValidator := application.NewAWSCredentialValidator(configuration)
	vpcStatusChecker := ec2.NewVPCStatusChecker(clientProvider)
	keyPairCreator := ec2.NewKeyPairCreator(clientProvider)
	keyPairDeleter := ec2.NewKeyPairDeleter(clientProvider, logger)
	keyPairChecker := ec2.NewKeyPairChecker(clientProvider)
	keyPairManager := ec2.NewKeyPairManager(keyPairCreator, keyPairChecker, logger)
	keyPairSynchronizer := ec2.NewKeyPairSynchronizer(keyPairManager)
	availabilityZoneRetriever := ec2.NewAvailabilityZoneRetriever(clientProvider)
	templateBuilder := templates.NewTemplateBuilder(logger)
	stackManager := cloudformation.NewStackManager(clientProvider, logger)
	infrastructureManager := cloudformation.NewInfrastructureManager(templateBuilder, stackManager)
	certificateUploader := iam.NewCertificateUploader(clientProvider)
	certificateDescriber := iam.NewCertificateDescriber(clientProvider)
	certificateDeleter := iam.NewCertificateDeleter(clientProvider)
	certificateManager := iam.NewCertificateManager(certificateUploader, certificateDescriber, certificateDeleter)
	certificateValidator := iam.NewCertificateValidator()

	// bosh-init
	tempDir, err := ioutil.TempDir("", "bosh-init")
	if err != nil {
		fail(err)
	}

	boshInitPath, err := exec.LookPath("bosh-init")
	if err != nil {
		fail(err)
	}

	cloudProviderManifestBuilder := manifests.NewCloudProviderManifestBuilder(stringGenerator)
	jobsManifestBuilder := manifests.NewJobsManifestBuilder(stringGenerator)
	boshinitManifestBuilder := manifests.NewManifestBuilder(manifests.ManifestBuilderInput{
		BOSHURL:        constants.BOSHURL,
		BOSHSHA1:       constants.BOSHSHA1,
		BOSHAWSCPIURL:  constants.BOSHAWSCPIURL,
		BOSHAWSCPISHA1: constants.BOSHAWSCPISHA1,
		StemcellURL:    constants.StemcellURL,
		StemcellSHA1:   constants.StemcellSHA1,
	},
		logger,
		sslKeyPairGenerator,
		stringGenerator,
		cloudProviderManifestBuilder,
		jobsManifestBuilder,
	)
	boshinitCommandBuilder := boshinit.NewCommandBuilder(boshInitPath, tempDir, os.Stdout, os.Stderr)
	boshinitDeployCommand := boshinitCommandBuilder.DeployCommand()
	boshinitDeleteCommand := boshinitCommandBuilder.DeleteCommand()
	boshinitDeployRunner := boshinit.NewCommandRunner(tempDir, boshinitDeployCommand)
	boshinitDeleteRunner := boshinit.NewCommandRunner(tempDir, boshinitDeleteCommand)
	boshinitExecutor := boshinit.NewExecutor(
		boshinitManifestBuilder, boshinitDeployRunner, boshinitDeleteRunner, logger,
	)

	// BOSH
	boshClientProvider := bosh.NewClientProvider()
	cloudConfigGenerator := bosh.NewCloudConfigGenerator()
	cloudConfigurator := bosh.NewCloudConfigurator(logger, cloudConfigGenerator)
	cloudConfigManager := bosh.NewCloudConfigManager(logger, cloudConfigGenerator)

	// Commands
	commandSet[commands.HelpCommand] = commands.NewUsage(os.Stdout)
	commandSet[commands.VersionCommand] = commands.NewVersion(Version, os.Stdout)
	commandSet[commands.UpCommand] = commands.NewUp(
		awsCredentialValidator, infrastructureManager, keyPairSynchronizer, boshinitExecutor,
		stringGenerator, cloudConfigurator, availabilityZoneRetriever, certificateDescriber,
		cloudConfigManager, boshClientProvider, envIDGenerator, stateStore,
		clientProvider,
	)
	commandSet[commands.DestroyCommand] = commands.NewDestroy(
		awsCredentialValidator, logger, os.Stdin, boshinitExecutor, vpcStatusChecker, stackManager,
		stringGenerator, infrastructureManager, keyPairDeleter, certificateDeleter, stateStore, stateValidator,
	)
	commandSet[commands.CreateLBsCommand] = commands.NewCreateLBs(
		logger, awsCredentialValidator, certificateManager, infrastructureManager,
		availabilityZoneRetriever, boshClientProvider, cloudConfigurator, cloudConfigManager, certificateValidator,
		uuidGenerator, stateStore, stateValidator,
	)
	commandSet[commands.UpdateLBsCommand] = commands.NewUpdateLBs(awsCredentialValidator, certificateManager,
		availabilityZoneRetriever, infrastructureManager, boshClientProvider, logger, certificateValidator, uuidGenerator,
		stateStore, stateValidator)
	commandSet[commands.DeleteLBsCommand] = commands.NewDeleteLBs(
		awsCredentialValidator, availabilityZoneRetriever, certificateManager,
		infrastructureManager, logger, cloudConfigurator, cloudConfigManager, boshClientProvider, stateStore,
		stateValidator,
	)
	commandSet[commands.LBsCommand] = commands.NewLBs(awsCredentialValidator, stateValidator, infrastructureManager, os.Stdout)
	commandSet[commands.DirectorAddressCommand] = commands.NewStateQuery(logger, stateValidator, commands.DirectorAddressPropertyName, func(state storage.State) string {
		return state.BOSH.DirectorAddress
	})
	commandSet[commands.DirectorUsernameCommand] = commands.NewStateQuery(logger, stateValidator, commands.DirectorUsernamePropertyName, func(state storage.State) string {
		return state.BOSH.DirectorUsername
	})
	commandSet[commands.DirectorPasswordCommand] = commands.NewStateQuery(logger, stateValidator, commands.DirectorPasswordPropertyName, func(state storage.State) string {
		return state.BOSH.DirectorPassword
	})
	commandSet[commands.DirectorCACertCommand] = commands.NewStateQuery(logger, stateValidator, commands.DirectorCACertPropertyName, func(state storage.State) string {
		return state.BOSH.DirectorSSLCA
	})
	commandSet[commands.BOSHCACertCommand] = commands.NewStateQuery(logger, stateValidator, commands.BOSHCACertPropertyName, func(state storage.State) string {
		fmt.Fprintln(os.Stderr, "'bosh-ca-cert' has been deprecated and will be removed in future versions of bbl, please use 'director-ca-cert'")
		return state.BOSH.DirectorSSLCA
	})
	commandSet[commands.SSHKeyCommand] = commands.NewStateQuery(logger, stateValidator, commands.SSHKeyPropertyName, func(state storage.State) string {
		return state.KeyPair.PrivateKey
	})
	commandSet[commands.EnvIDCommand] = commands.NewStateQuery(logger, stateValidator, commands.EnvIDPropertyName, func(state storage.State) string {
		return state.EnvID
	})

	app := application.New(commandSet, configuration, stateStore, usage)

	err = app.Run()
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "\n\n%s\n", err)
	os.Exit(1)
}
