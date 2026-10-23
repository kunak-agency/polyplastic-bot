package externalApi

import (
	"main/config"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/pkg/logger"
)

// ExternalAPI — клиенты внешних систем (только вызовы, без бизнес-логики).
type ExternalAPI struct {
	RAG *rag.Client
	// Max — nil, если MAX_BOT_TOKEN пуст: бот не стартует, HTTP и кабинет работают.
	Max max.Client
}

// New собирает клиентов. Отсутствие ключей не ошибка: сервис обязан подниматься на машине без
// доступов. Ошибка — только от явно заданного и негодного MAX_CA_FILE: без него бот всё равно не
// достучится до MAX, и лучше узнать об этом на старте.
func New(cfg config.Config) (*ExternalAPI, error) {
	ext := &ExternalAPI{RAG: rag.New(cfg.RAG.BaseURL, cfg.RAG.AskTimeout)}
	if !cfg.Max.Enabled() {
		if cfg.Bot.Fake {
			// development без токена: бот работает на подделке — машина времени и сид видны в
			// логе, в сеть ничего не уходит.
			logger.Warn("MAX_BOT_TOKEN пуст — бот на подделке MAX (только development), сообщения пишутся в лог")
			f := max.NewFake()
			f.Verbose = true
			ext.Max = f
			return ext, nil
		}
		logger.Warn("MAX_BOT_TOKEN пуст — бот MAX не стартует")
		return ext, nil
	}
	c, err := max.New(max.Config{Token: cfg.Max.Token, CAFile: cfg.Max.CAFile, PollTimeout: cfg.Max.PollTimeout})
	if err != nil {
		return nil, err
	}
	ext.Max = c
	return ext, nil
}
