package takeout

// Risk keyword groups scanned by the health-risk guard. Matching is
// case-insensitive on word boundaries.
var (
	medicalKeywords = newKeywordMatcher(
		"cure", "cures", "treatment", "treatments", "diagnose", "diagnosis",
		"medicine", "medication", "prescription", "heal",
	)
	extremeDietingKeywords = newKeywordMatcher(
		"starve", "starvation", "fasting", "extreme dieting", "eat nothing",
		"only drink water", "only water",
	)
	absoluteClaimKeywords = newKeywordMatcher(
		"guarantee", "guaranteed", "surefire", "healthiest",
		"definitely lose weight", "lose weight fast guaranteed",
	)
	specialGroupKeywords = newKeywordMatcher(
		"pregnant", "pregnancy", "diabetes", "diabetic", "hypertension",
		"high blood pressure", "minor", "minors", "underage", "child", "children",
	)
)

// RiskGuardService is the final compliance check after the LLM generated
// the reply: it scans the user input plus the assistant speech text for
// health-risk wording.
type RiskGuardService struct{}

// Check returns a pass result, or a block result whose rewrite suggestion
// replaces the generated speech text.
func (RiskGuardService) Check(userInput string, intent Intent, response ResponseResult) RiskGuardResult {
	reasons := []string{}
	allText := userInput + " " + response.SpeechText
	if intent == IntentHealthRisk {
		reasons = append(reasons, "HEALTH_RISK intent matched")
	}
	if medicalKeywords.matchesAny(allText) {
		reasons = append(reasons, "involves medical diagnosis or treatment promises")
	}
	if extremeDietingKeywords.matchesAny(allText) {
		reasons = append(reasons, "involves extreme dieting advice")
	}
	if absoluteClaimKeywords.matchesAny(allText) {
		reasons = append(reasons, "involves absolute health promises")
	}
	if specialGroupKeywords.matchesAny(allText) {
		reasons = append(reasons, "involves special groups or chronic disease risk")
	}
	if len(reasons) == 0 {
		return PassRiskGuardResult()
	}
	return BlockRiskGuardResult(reasons, ConservativeMessage)
}

// ConservativeMessage is the fixed conservative reply for high-risk turns.
const ConservativeMessage = "This question involves health or medical risk, and I cannot replace a doctor for diagnosis or treatment advice. From an everyday takeout perspective, choose light, balanced, and moderate meals; if symptoms are significant, or you are pregnant, nursing, or managing a chronic condition, please consult a doctor or a registered dietitian."
