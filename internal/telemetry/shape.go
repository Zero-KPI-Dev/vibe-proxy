package telemetry

import (
	"unicode/utf8"

	"github.com/a448582655/vibe-proxy/internal/ir"
)

func SummarizeRequestShape(request *ir.Request) RequestShapeSummary {
	if request == nil {
		return RequestShapeSummary{}
	}
	shape := RequestShapeSummary{
		InputMessageCount: len(request.Messages),
		InputToolCount:    len(request.Tools),
	}
	for _, message := range request.Messages {
		shape.InputBlockCount += len(message.Content)
		for _, block := range message.Content {
			switch block.Type {
			case ir.ContentImage:
				shape.InputImageCount++
			case ir.ContentText:
				shape.InputTextChars += utf8.RuneCountInString(block.Text)
			}
		}
	}
	return shape
}

func SummarizeResponseShape(response *ir.Response) RequestShapeSummary {
	if response == nil {
		return RequestShapeSummary{}
	}
	shape := RequestShapeSummary{OutputMessageCount: len(response.Messages)}
	for _, message := range response.Messages {
		shape.OutputBlockCount += len(message.Content)
		for _, block := range message.Content {
			switch block.Type {
			case ir.ContentText:
				shape.OutputTextChars += utf8.RuneCountInString(block.Text)
			case ir.ContentReasoning:
				shape.OutputReasoningChars += utf8.RuneCountInString(block.Text)
			case ir.ContentToolCall:
				shape.OutputToolCallCount++
			}
		}
	}
	return shape
}

func MergeResponseShape(input, output RequestShapeSummary) RequestShapeSummary {
	input.OutputMessageCount = output.OutputMessageCount
	input.OutputBlockCount = output.OutputBlockCount
	input.OutputToolCallCount = output.OutputToolCallCount
	input.OutputReasoningChars = output.OutputReasoningChars
	input.OutputTextChars = output.OutputTextChars
	return input
}
