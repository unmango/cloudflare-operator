package testing

//go:generate go tool mockgen -destination ./client.go -package testing ../../pkg/client/client.go Client
