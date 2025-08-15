# Tibia 7.7 Web Server
This is a simple web server designed to support [Tibia Game Server](https://github.com/fusion32/tibia-game). It is written in *Go* for simplicity and supports running over *HTTP* or *HTTPS*. Running over *HTTP* is not secure and should only be used for testing purposes. Running over *HTTPS* will require a valid certificate and key which may be acquired with [Let's Encrypt](https://letsencrypt.org/) free of cost, if you have a domain name.

## Compiling
The only dependency is an up to date [Go Compiler](https://go.dev/doc/install).
```
go build -o build/
```

## Running
Similar to the game server, the web server won't boot up if it's not able to connect to the [Query Manager](https://github.com/fusion32/tibia-querymanager). It is always recommended that the server is setup as a service. There is a *systemd* configuration file (`tibia-web.service`) in the repository that may be used for that purpose. The process is very similar to the one described in the [Game Server](https://github.com/fusion32/tibia-game) so I won't repeat myself here.
