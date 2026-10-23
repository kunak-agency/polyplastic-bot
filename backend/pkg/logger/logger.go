package logger

import "log"

func Info(format string, a ...any) { log.Printf("[INFO] "+format, a...) }

// Warn — деградация: работаем дальше, но данные пришли неполными. Error для этого слишком,
// а в потоке Info такая строка теряется.
func Warn(format string, a ...any)  { log.Printf("[WARN] "+format, a...) }
func Error(format string, a ...any) { log.Printf("[ERROR] "+format, a...) }
