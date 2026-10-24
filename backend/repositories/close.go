package repositories

// Close — закрыть пул соединений (тесты: каждый env открывает свой пул, и без закрытия пакет
// упирается в max_connections Postgres). Выключенная база — ничего.
func (r *Repos) Close() error {
	if !r.Enabled() {
		return nil
	}
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
