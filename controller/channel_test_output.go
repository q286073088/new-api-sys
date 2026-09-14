package controller

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const maxChannelTestOutputBytes = 32 << 10

type channelTestOutput struct {
	Prompt          string `json:"prompt"`
	Output          string `json:"output"`
	OutputTruncated bool   `json:"output_truncated,omitempty"`
}

// Only the channel-test controller calls this capture path. It consumes the
// response already held by its test recorder, never a live user's response.
func captureChannelTestOutput(prompt string, body []byte) channelTestOutput {
	var output, completed, items, snapshot channelTestText
	if gjson.ValidBytes(body) {
		appendChannelTestMessage(&output, gjson.ParseBytes(body))
	} else {
		for line := range bytes.SplitSeq(body, []byte{'\n'}) {
			payload, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
			payload = bytes.TrimSpace(payload)
			if !ok || !gjson.ValidBytes(payload) {
				continue
			}
			event := gjson.ParseBytes(payload)
			switch event.Get("type").String() {
			case "response.output_text.delta":
				output.append(event.Get("delta").String())
			case "response.output_text.done":
				completed.append(event.Get("text").String())
			case "response.output_item.done":
				appendChannelTestMessage(&items, event.Get("item"))
			case "response.completed", "response.incomplete", "response.failed":
				appendChannelTestMessage(&snapshot, event.Get("response"))
			case "content_block_start":
				output.append(event.Get("content_block.text").String())
			case "content_block_delta":
				output.append(event.Get("delta.text").String())
			case "message_start":
				appendChannelTestMessage(&output, event.Get("message"))
			default:
				appendChannelTestMessage(&output, event)
			}
		}
	}
	// Responses sends both deltas and a final snapshot; retain the answer once.
	answer := &output
	if snapshot.text.Len() > 0 {
		answer = &snapshot
	} else if output.text.Len() == 0 && completed.text.Len() > 0 {
		answer = &completed
	} else if output.text.Len() == 0 && items.text.Len() > 0 {
		answer = &items
	}
	return channelTestOutput{Prompt: prompt, Output: answer.text.String(), OutputTruncated: answer.truncated}
}

type channelTestText struct {
	text      strings.Builder
	truncated bool
}

func (out *channelTestText) append(text string) {
	if out.truncated {
		return
	}
	remaining := maxChannelTestOutputBytes - out.text.Len()
	if len(text) > remaining {
		out.truncated = true
		text = text[:remaining]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	out.text.WriteString(text)
}

func appendChannelTestContent(out *channelTestText, content gjson.Result) {
	if content.Type == gjson.String {
		out.append(content.String())
		return
	}
	for _, part := range content.Array() {
		// Preserve visible text only, excluding reasoning, binary and encrypted
		// compaction data, tool payloads and image URLs.
		if part.Get("thought").Bool() {
			continue
		}
		switch part.Get("type").String() {
		case "", "text", "output_text":
			out.append(part.Get("text").String())
		case "refusal":
			out.append(part.Get("refusal").String())
		}
	}
}

func appendChannelTestMessage(out *channelTestText, message gjson.Result) {
	if choices := message.Get("choices"); choices.IsArray() {
		choice := choices.Get("0")
		if content := choice.Get("message.content"); content.Exists() && content.Type != gjson.Null {
			appendChannelTestContent(out, content)
		} else if delta := choice.Get("delta.content"); delta.Exists() {
			appendChannelTestContent(out, delta)
		} else {
			out.append(choice.Get("text").String())
			out.append(choice.Get("message.refusal").String())
		}
		return
	}
	if candidates := message.Get("candidates"); candidates.IsArray() {
		appendChannelTestContent(out, candidates.Get("0.content.parts"))
		return
	}
	if output := message.Get("output"); output.IsArray() {
		for _, item := range output.Array() {
			if item.Get("type").String() == "message" {
				appendChannelTestContent(out, item.Get("content"))
			}
		}
		return
	}
	appendChannelTestContent(out, message.Get("content"))
}
