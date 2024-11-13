package wakey

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

type LLMProvider string

const (
	ProviderOpenAI LLMProvider = "openai"
)

type MessageModerator struct {
	config ModerationConfig
	llm    llms.Model
}

func NewMessageModerator(config ModerationConfig) (*MessageModerator, error) {
	var llm llms.Model
	var err error

	switch config.LLM.Provider {
	case ProviderOpenAI:
		options := []openai.Option{
			openai.WithToken(config.LLM.APIKey),
			openai.WithModel(config.LLM.Model),
		}
		if config.LLM.BaseURL != "" {
			options = append(options, openai.WithBaseURL(config.LLM.BaseURL))
		}
		llm, err = openai.New(options...)

	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	if config.Prompt == "" {
		config.Prompt = defaultSystemPrompt
	}

	return &MessageModerator{
		config: config,
		llm:    llm,
	}, nil
}

func (m *MessageModerator) CheckMessage(ctx context.Context, message string) (float64, error) {
	var lastErr error
	retries := 0

	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				llms.TextPart(m.config.Prompt),
			},
		},
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextPart(message),
			},
		},
	}

	for retries <= m.config.LLM.MaxRetries {
		response, err := m.llm.GenerateContent(ctx, messages,
			llms.WithTemperature(m.config.Temp),
			llms.WithMaxTokens(m.config.MaxTok),
		)

		if err == nil {
			if len(response.Choices) == 0 {
				return 0, fmt.Errorf("empty response from LLM")
			}

			responseText := response.Choices[0].Content
			return parseScore(responseText)
		}

		var waitTime time.Duration

		// Check if it's a rate limit error
		if timeout, ok := parseRateLimit(err); ok {
			waitTime = timeout
		} else if isServiceUnavailable(err) {
			// Wait 3 seconds for service unavailable errors
			waitTime = 3 * time.Second
		} else {
			// If it's not a rate limit or service unavailable error, return immediately
			return 0, fmt.Errorf("LLM generation failed: %w", err)
		}

		// Check if context is cancelled before waiting
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(waitTime):
			retries++
			lastErr = err
			continue
		}
	}

	return 0, fmt.Errorf("max retries exceeded, last error: %w", lastErr)
}

func parseRateLimit(err error) (time.Duration, bool) {
	re := regexp.MustCompile(`Please try again in\s+(\d+)`)
	match := re.FindStringSubmatch(err.Error())
	if len(match) < 2 {
		return 0, false
	}

	timeout, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, false
	}

	timeout++
	return time.Duration(timeout) * time.Second, true
}

func isServiceUnavailable(err error) bool {
	return strings.Contains(err.Error(), "Service Unavailable")
}

func parseScore(response string) (float64, error) {
	re := regexp.MustCompile(`FINAL SCORE:\s*(\d+\.?\d*)`)
	match := re.FindStringSubmatch(response)
	if len(match) < 2 {
		return 0, fmt.Errorf("invalid response format")
	}

	score, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse score: %w", err)
	}

	return score, nil
}

const defaultSystemPrompt = `You are a content moderation AI assistant. Your task is to analyze messages for inappropriate content and assign a violation probability score from 0.0 to 1.0. Reply only in English.

Follow this analysis structure:
1. First, list any detected issues in these categories:
   - Toxicity and hostile behavior
   - Insults and harassment
   - Hate speech and discrimination
   - Threats and violence
   - Illegal content
   - Adult content
   - Spam/malicious
   - Personal information
   - Cyberbullying
   - Misinformation

2. Rate the severity of each detected issue (if any):
   - None (0.0)
   - Mild (0.3)
   - Moderate (0.5)
   - Severe (0.7)
   - Extreme (1.0)

3. Consider contextual factors:
   - Intent (casual/malicious)
   - Potential harm
   - Target audience impact
   - Cultural context

4. Calculate final score:
   - 0.0-0.3: Safe content
   - 0.3-0.7: Potentially problematic
   - 0.7-1.0: Clearly violating

Respond in this exact format:

TRANSLATION:
[Translate the text into English]

ANALYSIS:
[Write your detailed analysis here]

DETECTED ISSUES:
[List main issues found]

SEVERITY ASSESSMENT:
[List severity of each issue]

CONTEXTUAL FACTORS:
[List relevant context]

FINAL SCORE: X.XX`
