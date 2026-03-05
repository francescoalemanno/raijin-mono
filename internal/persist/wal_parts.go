package persist

import libagent "github.com/francescoalemanno/raijin-mono/libagent"

func messageToWalMsg(m libagent.Message) walMessage {
	switch msg := m.(type) {
	case *libagent.UserMessage:
		return walMessage{Kind: "user", User: libagent.CloneMessage(msg).(*libagent.UserMessage)}
	case *libagent.AssistantMessage:
		clone := libagent.CloneMessage(msg).(*libagent.AssistantMessage)
		clone.Content = nil
		clone.Error = nil
		return walMessage{Kind: "assistant", Assistant: clone}
	case *libagent.ToolResultMessage:
		return walMessage{Kind: "tool_result", ToolResult: libagent.CloneMessage(msg).(*libagent.ToolResultMessage)}
	default:
		return walMessage{}
	}
}

func walMsgToMessage(wm walMessage) (libagent.Message, bool) {
	switch wm.Kind {
	case "user":
		if wm.User == nil {
			return nil, false
		}
		return libagent.CloneMessage(wm.User), true
	case "assistant":
		if wm.Assistant == nil {
			return nil, false
		}
		return libagent.CloneMessage(wm.Assistant), true
	case "tool_result":
		if wm.ToolResult == nil {
			return nil, false
		}
		return libagent.CloneMessage(wm.ToolResult), true
	default:
		return nil, false
	}
}
