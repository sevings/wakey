package wakey_test

import (
	"context"
	"os"
	"testing"
	"time"
	"wakey/internal/wakey"

	"github.com/stretchr/testify/require"
)

func setupModerator(t *testing.T) *wakey.MessageModerator {
	config, err := wakey.LoadConfig()
	require.NoError(t, err)
	mod, err := wakey.NewMessageModerator(config.Moderation)
	require.NoError(t, err)
	require.NotNil(t, mod)
	return mod
}

func TestMessageModerator(t *testing.T) {
	if os.Getenv("WAKEY_ALL_TESTS") == "" {
		t.Skip("Skipping Moderator test because WAKEY_ALL_TESTS is not set")
	}

	// Context with timeout for all tests
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	tests := []struct {
		name          string
		message       string
		expectedRange [2]float64 // min and max expected score
		expectError   bool
	}{
		{
			name:          "clean message",
			message:       "Привет! Как дела? Хорошей погоды и хорошего настроения!",
			expectedRange: [2]float64{0.0, 0.3},
		},
		{
			name:          "mild toxicity",
			message:       "Ты что, совсем глупый? Почитай для начала инструкцию.",
			expectedRange: [2]float64{0.3, 0.6},
		},
		{
			name:          "aggressive message",
			message:       "Да пошел ты! Ненавижу таких идиотов как ты!",
			expectedRange: [2]float64{0.7, 1.0},
		},
		{
			name:          "hate speech",
			message:       "Все [национальность] должны быть депортированы! Они не заслуживают жить здесь!",
			expectedRange: [2]float64{0.8, 1.0},
		},
		{
			name:          "spam message",
			message:       "СРОЧНО! ЗАРАБОТАЙ 1000000 РУБЛЕЙ ЗА НЕДЕЛЮ! КЛИКАЙ СЮДА: http://fake-scam.com",
			expectedRange: [2]float64{0.6, 1.0},
		},
		{
			name:          "personal information",
			message:       "Телефон Иванова: +7999123456, проживает по адресу ул. Ленина 123-45",
			expectedRange: [2]float64{0.5, 0.9},
		},
		{
			name:          "threats",
			message:       "Я знаю где ты живешь. Смотри, как бы чего не случилось.",
			expectedRange: [2]float64{0.7, 1.0},
		},
		{
			name:          "misinformation",
			message:       "COVID-19 лечится водкой с перцем! Это доказано учеными! Маски не нужны!",
			expectedRange: [2]float64{0.6, 0.9},
		},
		{
			name:          "mixed content",
			message:       "Привет! Подскажите пожалуйста, где найти документацию? Задолбали тупые вопросы.",
			expectedRange: [2]float64{0.3, 0.5},
		},
		{
			name:        "empty message",
			message:     "",
			expectError: true,
		},
		{
			name:        "very long message",
			message:     string(make([]rune, 10000)), // 10K characters
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := setupModerator(t)
			score, err := mod.CheckMessage(ctx, tt.message)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.GreaterOrEqual(t, score, tt.expectedRange[0], "Score should be >= min expected")
			require.LessOrEqual(t, score, tt.expectedRange[1], "Score should be <= max expected")
		})
	}
}
