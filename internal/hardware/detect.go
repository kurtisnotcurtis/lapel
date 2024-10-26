package hardware

import (
	"context"
	"fmt"
)

// Detect detects system hardware information based on the current OS.
func Detect(ctx context.Context) (*Info, error) {
	// Always try Linux first (works for native Linux and WSL)
	linuxDet := NewLinuxDetector()
	info, err := linuxDet.Detect(ctx)
	if err == nil && info.OS == "linux" {
		return info, nil
	}
	fmt.Printf("  [DEBUG] Linux detection failed: %v\n", err)

	// Fall back to Windows (for native Windows .exe execution)
	winDet := NewWindowsDetector()
	info, err = winDet.Detect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect hardware: linux=%v, windows=%v", err, err)
	}
	return info, nil
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
