package deployd

const (
	// Default provisioning provider for vms is OpenNebula.
	// This is just an endpoint for Megam. We could have openstack, chef, salt, puppet etc.
	DefaultProvider = "one"
	// DefaultOneEndpoint is the default address that the service binds to an IaaS (OpenNebula).
	DefaultOneEndpoint = "http://localhost:2633/RPC2"

	// DefaultUserid the default userid for the IaaS service (OpenNebula).
	DefaultOneUserid = "oneadmin"

	// DefaultOnePassword is the default password for the IaaS service (OpenNebula).
	DefaultOnePassword = "password"

	// DefaultOneTemplate is the default template for the IaaS service (OpenNebula).
	DefaultOneTemplate = "megam"

	// DefaultOneZone is the default zone for the IaaS service (OpenNebula).
	DefaultOneZone = "plano01"
)

type Config struct {
	Provider    string `toml:"provider"`
	OneEndPoint string `toml:"one_endpoint"`
	OneUserid   string `toml:"one_userid"`
	OnePassword string `toml:"one_password"`
	OneTemplate string `toml:"one_template"`
	OneZone     string `toml:"one_zone"`
	Certificate string `toml:"certificate"`
}

func (c Config) String() string {
	table := NewTable()
	table.AddRow(Row{Colorfy("Config:", "white", "", "bold"), Colorfy("Deployd", "green", "", "")})
	table.AddRow(Row{"Provider", c.Provider})
	table.AddRow(Row{"Endpoint", c.OneEndPoint})
	table.AddRow(Row{"Userid", c.OneUserid})
	table.AddRow(Row{"Password", c.OnePassword})
	table.AddRow(Row{"Template", c.OneTemplate})
	table.AddRow(Row{"", ""})
	return table.String()
}

func NewConfig() *Config {
	return &Config{
		Provider:    DefaultProvider,
		OneEndPoint: DefaultOneEndpoint,
		OneUserid:   DefaultOneUserid,
		OnePassword: DefaultOnePassword,
		OneTemplate: DefaultOneTemplate,
		OneZone:     DefaultOneZone,
		Certificate: "/var/lib/megam/megamd/id_rsa.pub",
	}
}
