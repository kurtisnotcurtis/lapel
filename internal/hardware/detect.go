package hardware

import (
	"context"
	"fmt"
	"runtime"
)

// Detect detects system hardware information based on the current OS.
func Detect(ctx context.Context) (*Info, error) {
	if runtime.GOOS == "windows" {
		winDet := NewWindowsDetector()
		info, err := winDet.Detect(ctx)
		if err != nil {
			return nil, fmt.Errorf("detecting Windows hardware: %w", err)
		}
		return info, nil
	}

	linuxDet := NewLinuxDetector()
	linuxInfo, linuxErr := linuxDet.Detect(ctx)
	if linuxErr == nil && linuxInfo.OS == "linux" {
		return linuxInfo, nil
	}

	winDet := NewWindowsDetector()
	winInfo, winErr := winDet.Detect(ctx)
	if winErr != nil {
		return nil, fmt.Errorf("failed to detect hardware: linux=%v, windows=%v", linuxErr, winErr)
	}
	return winInfo, nil
}

// Recommend returns a model recommendation based on detected hardware.
func Recommend(ctx context.Context) (*RecommendedModel, error) {
	info, err := Detect(ctx)
	if err != nil {
		return nil, fmt.Errorf("detecting hardware: %w", err)
	}

	recommender := NewDefaultRecommender()
	return recommender.Recommend(info), nil
}
