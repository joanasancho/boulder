package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jmhodges/clock"

	"github.com/letsencrypt/boulder/bdns"
	"github.com/letsencrypt/boulder/cmd"
	"github.com/letsencrypt/boulder/core"
	bgrpc "github.com/letsencrypt/boulder/grpc"
	"github.com/letsencrypt/boulder/metrics"
	"github.com/letsencrypt/boulder/policy"
	"github.com/letsencrypt/boulder/ra"
	"github.com/letsencrypt/boulder/rpc"
)

const clientName = "RA"

type config struct {
	RA struct {
		cmd.ServiceConfig
		cmd.HostnamePolicyConfig

		RateLimitPoliciesFilename string

		MaxConcurrentRPCServerRequests int64

		MaxContactsPerRegistration int

		// UseIsSafeDomain determines whether to call VA.IsSafeDomain
		UseIsSafeDomain bool // TODO: remove after va IsSafeDomain deploy

		// The number of times to try a DNS query (that has a temporary error)
		// before giving up. May be short-circuited by deadlines. A zero value
		// will be turned into 1.
		DNSTries int

		VAService *cmd.GRPCClientConfig

		MaxNames     int
		DoNotForceCN bool

		// Controls behaviour of the RA when asked to create a new authz for
		// a name/regID that already has a valid authz. False preserves historic
		// behaviour and ignores the existing authz and creates a new one. True
		// instructs the RA to reuse the previously created authz in lieu of
		// creating another.
		ReuseValidAuthz bool
	}

	*cmd.AllowedSigningAlgos

	PA cmd.PAConfig

	cmd.StatsdConfig

	cmd.SyslogConfig

	Common struct {
		DNSResolver               string
		DNSTimeout                string
		DNSAllowLoopbackAddresses bool
	}
}

func main() {
	configFile := flag.String("config", "", "File path to the configuration file for this service")
	flag.Parse()
	if *configFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	var c config
	err := cmd.ReadJSONFile(*configFile, &c)
	cmd.FailOnError(err, "Reading JSON config file into config structure")

	go cmd.DebugServer(c.RA.DebugAddr)

	stats, logger := cmd.StatsAndLogging(c.StatsdConfig, c.SyslogConfig)
	defer logger.AuditPanic()
	logger.Info(cmd.VersionString(clientName))

	// Validate PA config and set defaults if needed
	cmd.FailOnError(c.PA.CheckChallenges(), "Invalid PA configuration")

	pa, err := policy.New(c.PA.Challenges)
	cmd.FailOnError(err, "Couldn't create PA")

	if c.RA.HostnamePolicyFile == "" {
		cmd.FailOnError(fmt.Errorf("HostnamePolicyFile must be provided."), "")
	}
	err = pa.SetHostnamePolicyFile(c.RA.HostnamePolicyFile)
	cmd.FailOnError(err, "Couldn't load hostname policy file")

	go cmd.ProfileCmd("RA", stats)

	amqpConf := c.RA.AMQP
	var vac core.ValidationAuthority
	if c.RA.VAService != nil {
		conn, err := bgrpc.ClientSetup(c.RA.VAService)
		cmd.FailOnError(err, "Unable to create VA client")
		vac = bgrpc.NewValidationAuthorityGRPCClient(conn)
	} else {
		vac, err = rpc.NewValidationAuthorityClient(clientName, amqpConf, stats)
		cmd.FailOnError(err, "Unable to create VA client")
	}

	cac, err := rpc.NewCertificateAuthorityClient(clientName, amqpConf, stats)
	cmd.FailOnError(err, "Unable to create CA client")

	sac, err := rpc.NewStorageAuthorityClient(clientName, amqpConf, stats)
	cmd.FailOnError(err, "Unable to create SA client")

	rai := ra.NewRegistrationAuthorityImpl(
		clock.Default(),
		logger,
		stats,
		c.RA.MaxContactsPerRegistration,
		c.AllowedSigningAlgos.KeyPolicy(),
		c.RA.MaxNames,
		c.RA.DoNotForceCN,
		c.RA.ReuseValidAuthz)

	policyErr := rai.SetRateLimitPoliciesFile(c.RA.RateLimitPoliciesFilename)
	cmd.FailOnError(policyErr, "Couldn't load rate limit policies file")
	rai.PA = pa

	raDNSTimeout, err := time.ParseDuration(c.Common.DNSTimeout)
	cmd.FailOnError(err, "Couldn't parse RA DNS timeout")
	scoped := metrics.NewStatsdScope(stats, "RA", "DNS")
	dnsTries := c.RA.DNSTries
	if dnsTries < 1 {
		dnsTries = 1
	}
	if !c.Common.DNSAllowLoopbackAddresses {
		rai.DNSResolver = bdns.NewDNSResolverImpl(
			raDNSTimeout,
			[]string{c.Common.DNSResolver},
			nil,
			scoped,
			clock.Default(),
			dnsTries)
	} else {
		rai.DNSResolver = bdns.NewTestDNSResolverImpl(
			raDNSTimeout,
			[]string{c.Common.DNSResolver},
			scoped,
			clock.Default(),
			dnsTries)
	}

	rai.VA = vac
	rai.CA = cac
	rai.SA = sac

	ras, err := rpc.NewAmqpRPCServer(amqpConf, c.RA.MaxConcurrentRPCServerRequests, stats, logger)
	cmd.FailOnError(err, "Unable to create RA RPC server")
	err = rpc.NewRegistrationAuthorityServer(ras, rai, logger)
	cmd.FailOnError(err, "Unable to setup RA RPC server")

	err = ras.Start(amqpConf)
	cmd.FailOnError(err, "Unable to run RA RPC server")
}
