package config

const (
	DefaultPanelGitHubRepository = "https://github.com/spock-wen/codeProxy"
	DefaultPprofAddr             = "127.0.0.1:8316"
	DefaultAutoUpdateChannel     = "main"
	DefaultAutoUpdateRepository  = "https://github.com/spock-wen/CliRelay"
	DefaultAutoUpdateDockerImage = "registry.cn-hangzhou.aliyuncs.com/hihope_clirelay/clirelay"
	DefaultAutoUpdateUpdaterURL  = "http://clirelay-updater:8320"

	// EnvAuthPath overrides auth-dir with the path visible inside the running container/process.
	EnvAuthPath = "AUTH_PATH"
)
