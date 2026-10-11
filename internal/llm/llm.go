// Package llm defines the shared commit-message generation contract used by
// every LLM backend omc supports (Ollama, OpenAI-compatible endpoints).
//
// The two prompts below are the single source of truth: every backend sends
// the exact same text to its model so the commit messages are comparable
// regardless of which backend answered. A backend implements Generator by
// carrying the prompt to its own API and returning the model's text.
package llm

import (
	"context"
	"strconv"
	"strings"
)

// maxSectionLines is the threshold above which a per-file diff section is
// considered too large to send to the model in full.
const maxSectionLines = 200

// truncatedSectionLines is the maximum number of lines kept from an
// oversized section — the first half of this budget is taken from the top
// of the section and the second half from the bottom, preserving the file
// header and the overall shape of the change.
const truncatedSectionLines = 100

// DetailPrompt builds the prompt that asks the model for a detailed,
// conversational commit message explaining the staged diff. diffStat is a
// compact file-level summary; diff is the full staged diff. Backends pass
// these verbatim to their model.
//
// The diff is first run through truncateDiff so that no single file section
// longer than maxSectionLines overwhelms the model's context window: oversized
// sections are cut to truncatedSectionLines with a visible truncation marker.
func DetailPrompt(diff, diffStat string) string {
	return "You are a git commit message assistant.\n" +
		"Write a detailed commit message explaining what changed and why, based only on the diff below.\n" +
		"Use plain markdown bullets. Be specific. Do not mention this prompt.\n\n" +
		"DIFF STAT:\n" + diffStat + "\n\nDIFF:\n" + truncateDiff(diff)
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

// truncateDiff limits every per-file section of a unified diff to at most
// truncatedSectionLines lines when the section exceeds maxSectionLines.
// Sections at or below the threshold are passed through unchanged. A cut
// section keeps the first and last portions of the change so the model still
// sees the file header, the hunk markers, and representative context from both
// the top and bottom of the change. A clearly marked line is inserted at the
// cut point so the model knows content was elided.
func truncateDiff(diff string) string {
	if diff == "" {
		return diff
	}
	lines := strings.Split(diff, "\n")

	var (
		out       []string
		start     int
		inSection bool
	)

	flush := func(section []string) {
		if len(section) <= maxSectionLines {
			out = append(out, section...)
			return
		}
		keep := truncatedSectionLines
		top := keep / 2
		bottom := keep - top
		out = append(out, section[:top]...)
		out = append(out, "")
		out = append(out, " ... (diff truncated: section had "+
			strconv.Itoa(len(section))+" lines, showing "+strconv.Itoa(keep)+") ...")
		out = append(out, "")
		out = append(out, section[len(section)-bottom:]...)
	}

	for i, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			if inSection {
				flush(lines[start:i])
			}
			start = i
			inSection = true
		}
	}
	if inSection {
		flush(lines[start:])
	} else {
		// No per-file headers: treat the whole text as one section.
		flush(lines)
	}

	return strings.Join(out, "\n")
}
