package meta

import (
	"os"
	"time"

	"github.com/megamsys/libgo/cmd"
	"github.com/megamsys/megamd/toml"
)

const (
	// DefaultHostname is the default hostname if one is not provided.
	DefaultHostname = "localhost"

	// DefaultBindAddress is the default address to bind to.
	DefaultBindAddress = ":9999"

	// DefaultRiak is the default riak if one is not provided.
	DefaultRiak = "localhost:8087"

	// DefaultApi is the default megam gateway if one is not provided.
	DefaultApi = "https://api.megam.io/v2"

	// DefaultAMQP is the default rabbitmq if one is not provided.
	DefaultAMQP = "amqp://guest:guest@localhost:5672/"

	// DefaultHeartbeatTimeout is the default heartbeat timeout for the store.
	DefaultHeartbeatTimeout = 1000 * time.Millisecond

	// DefaultElectionTimeout is the default election timeout for the store.
	DefaultElectionTimeout = 1000 * time.Millisecond

	// DefaultLeaderLeaseTimeout is the default leader lease for the store.
	DefaultLeaderLeaseTimeout = 500 * time.Millisecond
)

// Config represents the meta configuration.
type Config struct {
	Home               string        `toml:home`
	Dir                string        `toml:"dir"`
	Hostname           string        `toml:"hostname"`
	BindAddress        string        `toml:bind_address`
	Riak               string        `toml:"riak"`
	Api                string        `toml:"api"`
	AMQP               string        `toml:"amqp"`
	Peers              []string      `toml:"-"`
	ElectionTimeout    toml.Duration `toml:"election-timeout"`
	HeartbeatTimeout   toml.Duration `toml:"heartbeat-timeout"`
	LeaderLeaseTimeout toml.Duration `toml:"leader-lease-timeout"`
}

func (c Config) String() string {
	table := NewTable()
	table.AddRow(Row{Colorfy("Config:", "white", "", "bold"), Colorfy("Meta", "green", "", "")})
	table.AddRow(Row{"Home", c.Home})
	table.AddRow(Row{"Dir", c.Dir})
	table.AddRow(Row{"Riak", c.Riak})
	table.AddRow(Row{"API", c.Api})
	table.AddRow(Row{"AMQP", c.AMQP})
	table.AddRow(Row{"Hostname", c.Hostname})
	table.AddRow(Row{"", ""})
	return table.String()
}

func NewConfig() *Config {
	var homeDir string
	// By default, store logs, meta and load conf files in current users home directory
	if os.Getenv("MEGAM_HOME") != "" {
		homeDir = os.Getenv("MEGAM_HOME")
	} else if u, err := user.Current(); err == nil {
		homeDir = u.HomeDir
	} else {
		return nil, fmt.Errorf("failed to determine home directory")
	}

	defaultDir := filepath.Join(homeDir, "megamd/meta")

	// Config represents the configuration format for the megamd.
	return &Config{
		Home:               homeDir,
		Dir:                defaultDir,
		Hostname:           DefaultHostname,
		BindAddress:        DefaultBindAddress,
		Riak:               DefaultRiak,
		Api:                DefaultApi,
		AMQP:               DefaultAMQP,
		ElectionTimeout:    toml.Duration(DefaultElectionTimeout),
		HeartbeatTimeout:   toml.Duration(DefaultHeartbeatTimeout),
		LeaderLeaseTimeout: toml.Duration(DefaultLeaderLeaseTimeout),
	}
}
