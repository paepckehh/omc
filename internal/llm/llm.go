// Package llm defines the shared commit-message generation contract used by
// every LLM backend omc supports (Ollama, OpenAI-compatible endpoints).
//
// The two prompts below are the single source of truth: every backend sends
// the exact same text to its model so the commit messages are comparable
// regardless of which backend answered. A backend implements Generator by
// carrying the prompt to its own API and returning the model's text.
package llm

import "context"

// DetailPrompt builds the prompt that asks the model for a detailed,
// conversational commit message explaining the staged diff. diffStat is a
// compact file-level summary; diff is the full staged diff. Backends pass
// these verbatim to their model.
func DetailPrompt(diff, diffStat string) string {
	return "You are a git commit message assistant.\n" +
		"Write a detailed commit message explaining what changed and why, based only on the diff below.\n" +
		"Use plain markdown bullets. Be specific. Do not mention this prompt.\n\n" +
		"DIFF STAT:\n" + diffStat + "\n\nDIFF:\n" + diff
}

// TLDRPrompt builds the prompt that condenses a detailed commit message into
// a single TL;DR-style line suitable as a git commit subject.
func TLDRPrompt(detail string) string {
	return "Summarize the following git commit message into one TL;DR line " +
		"(max 72 chars, no leading 'TL;DR', no trailing period, imperative mood).\n\n" +
		detail
}

// Generator is the commit-message backend contract. Each backend (Ollama,
// OpenAI-compatible) implements it; the pipeline probes backends in order
// and uses the first one that is Available, then calls DescribeDetail followed
// by SummarizeTLDR to produce the subject and body.
type Generator interface {
	// Available reports whether the backend answers a cheap probe. A false
	// result triggers the next backend in the fallback chain.
	Available(ctx context.Context) bool
	// DescribeDetail returns a detailed, conversational commit message
	// explaining the staged diff.
	DescribeDetail(ctx context.Context, diff, diffStat string) (string, error)
	// SummarizeTLDR condenses a detailed commit message into a single
	// TL;DR-style line suitable as a git commit subject.
	SummarizeTLDR(ctx context.Context, detail string) (string, error)
}
