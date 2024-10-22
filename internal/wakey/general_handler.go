package wakey

import (
	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

type GeneralHandler struct {
	db  *DB
	log *zap.SugaredLogger
}

func NewGeneralHandler(db *DB, log *zap.SugaredLogger) *GeneralHandler {
	return &GeneralHandler{
		db:  db,
		log: log,
	}
}

func (gh *GeneralHandler) Actions() []string {
	return []string{btnDoNothing}
}

func (gh *GeneralHandler) HandleAction(c tele.Context, action string) error {
	switch action {
	case btnDoNothing:
		return c.Edit("Хорошо, до свидания! Если вам что-то понадобится, просто напишите мне.")
	default:
		gh.log.Errorw("unexpected action for GeneralHandler", "action", action)
		return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
	}
}

func (gh *GeneralHandler) States() []UserState {
	return []UserState{}
}

func (gh *GeneralHandler) HandleState(c tele.Context, state UserState) error {
	gh.log.Errorw("unexpected state for GeneralHandler", "state", state)
	return c.Edit("Неизвестное действие. Пожалуйста, попробуйте еще раз.")
}
