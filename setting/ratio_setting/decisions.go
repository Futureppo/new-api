package ratio_setting

// Decisions pricing is independent of the same model's generative endpoints.
const DecisionsModelSuffix = "-openai-decisions"

func DecisionsBillingModel(model string) string {
	return model + DecisionsModelSuffix
}
