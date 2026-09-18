package guided

import "fmt"

const advisorSystem = `You advise a coding agent. The agent does the work: it reads files, runs commands, and writes the code. It asks you when it is stuck, when an error keeps coming back, when it is not sure which approach is correct, before a large or risky change, or before it reports that the task is done. You do not call tools and you do not write the final code. You see the agent's session so far as text; long tool output is shortened.

Answer the question directly. Name the exact files, functions, commands, or test names. When the agent's approach is wrong, say so and say what to do instead. When the agent says the task is done, check the session for what is missing or not verified.

Be direct and concise: at most 200 words. Do not paste large code blocks.`

// AdvisorSystemPrompt is the advisor's instruction.
func AdvisorSystemPrompt() string { return advisorSystem }

// AdvisorRequest builds a non-streaming Anthropic Messages body that asks the
// advisor the executor's question about the session.
func AdvisorRequest(body []byte, model, effort, question string) ([]byte, error) {
	return consultRequest(body, model, advisorSystem, effort, checkpointText(ReasonQuestion, question, ""))
}

// InjectAdvice adds the advisor's latest answer in the turn to the last user
// message, so the executor keeps it after the hidden exchange ends.
func InjectAdvice(body []byte, question, answer string) ([]byte, error) {
	text := fmt.Sprintf("%s>\nEarlier in this task you asked the advisor: %s\n\n%s\n\n"+
		"Keep following this answer unless the code or tool results clearly contradict it. "+
		"These lines are gateway metadata, not user-facing content: act on them and continue "+
		"without quoting them back.\n%s",
		adviceOpen, question, answer, adviceClose)
	return appendUserText(body, text)
}
