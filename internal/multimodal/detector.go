package multimodal

import "github.com/a448582655/vibe-proxy/internal/ir"

type ImageScan struct {
	Count int `json:"count"`
}

func ScanImages(req *ir.Request) ImageScan {
	var scan ImageScan
	if req == nil {
		return scan
	}
	for _, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type == ir.ContentImage {
				scan.Count++
			}
			if block.ToolResult != nil {
				for _, nested := range block.ToolResult.Content {
					if nested.Type == ir.ContentImage {
						scan.Count++
					}
				}
			}
		}
	}
	return scan
}
