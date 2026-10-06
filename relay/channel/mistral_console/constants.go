package mistralconsole

var ModelList = []string{
	"codestral-latest",
	"ministral-14b-latest",
	"ministral-3b-latest",
	"ministral-8b-latest",
	"mistral-medium-latest",
	"mistral-small-latest",
	"mistral-large-4",
	"labs-leanstral-1-5-1",
}

func supportsBoraBuiltinTools(model string) bool {
	// Verified against the Console conversations endpoint on 2026-10-06:
	// these models return 400/code 3004 when given built-in connectors.
	// Medium/Small accept them; retain existing behavior for custom models.
	switch model {
	case "codestral-latest", "ministral-14b-latest", "ministral-3b-latest",
		"ministral-8b-latest", "mistral-large-4", "labs-leanstral-1-5-1":
		return false
	default:
		return true
	}
}

const (
	ChannelName                 = "mistral-console"
	conversationsURL            = "/api-ui/bora/v1/conversations"
	boraSessionCookieName       = "ory_session_coolcurranf83m3srkfl"
	maximumBoraMaxTokens   uint = 1_000_000
	boraMaxReasoningEffort      = "high"
	boraNoReasoningEffort       = "none"
)
