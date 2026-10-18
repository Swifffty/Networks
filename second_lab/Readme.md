# Запуск
## Сервер
go run ./server <порт>

Пример:

go run ./server 5000

## Клиент
go run ./client <путь к файлу> <хост> <порт>

Хост может быть DNS-именем, IPv4- или IPv6-адресом. Примеры:

go run ./client photo.jpg localhost 5000
go run ./client /home/user/docs/report.pdf 192.168.1.5 5000
go run ./client video.mp4 ::1 5000
 

