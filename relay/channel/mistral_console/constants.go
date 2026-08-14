package mistralconsole

var ModelList = []string{
	"glm-5-2",
}

const (
	ChannelName                 = "mistral-console"
	conversationsURL            = "/api-ui/bora/v1/conversations"
	boraSessionCookieName       = "ory_session_coolcurranf83m3srkfl"
	defaultBoraMaxTokens   uint = 1_000_000
	boraMaxReasoningEffort      = "high"
)
