package model

import (
	"fmt"
	"os"
)

// FromEnv builds the default Model from environment. Provider is selected by
// FORGE_MODEL_PROVIDER ("anthropic" | "azure"). This is the one place vendor
// choice is wired; agents never see it.
//
//	anthropic: ANTHROPIC_API_KEY, ANTHROPIC_MODEL (optional)
//	azure:     AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_DEPLOYMENT, AZURE_OPENAI_KEY
func FromEnv() (Model, error) {
	switch os.Getenv("FORGE_MODEL_PROVIDER") {
	case "azure":
		ep := os.Getenv("AZURE_OPENAI_ENDPOINT")
		dep := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
		key := os.Getenv("AZURE_OPENAI_KEY")
		if ep == "" || dep == "" || key == "" {
			return nil, fmt.Errorf("azure provider needs AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_DEPLOYMENT, AZURE_OPENAI_KEY")
		}
		return NewAzureOpenAI(ep, dep, key), nil
	case "anthropic", "":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("anthropic provider needs ANTHROPIC_API_KEY")
		}
		return NewAnthropic(key, os.Getenv("ANTHROPIC_MODEL")), nil
	default:
		return nil, fmt.Errorf("unknown FORGE_MODEL_PROVIDER %q", os.Getenv("FORGE_MODEL_PROVIDER"))
	}
}
