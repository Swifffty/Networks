Установка

В папке проекта:

go mod tidy

Команда скачает Fyne, nbio и их зависимости. Если зависимостей нет в go.mod, добавьте их явно:

go get fyne.io/fyne/v2@latest
go get github.com/lesismal/nbio@v1.7.2
go mod tidy

Ключи передаются через переменные окружения GRAPHHOPPER_KEY и WEATHER_KEY.

export GRAPHHOPPER_KEY="ваш_ключ"
export WEATHER_KEY="ваш_ключ"

запуск
go run .