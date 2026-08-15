package mistralconsole

var ModelList = []string{
	"glm-5-2",
}

const (
	ChannelName                 = "mistral-console"
	conversationsURL            = "/api-ui/bora/v1/conversations"
	boraSessionCookieName       = "ory_session_coolcurranf83m3srkfl"
	defaultBoraMaxTokens   uint = 256 * 1024
	maximumBoraMaxTokens   uint = 1_000_000
	boraMaxReasoningEffort      = "high"
	boraNoReasoningEffort       = "none"
)
