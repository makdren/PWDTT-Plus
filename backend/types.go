package backend

// ConnectParams — параметры подключения из frontend.
type ConnectParams struct {
	PeerAddr    string   `json:"peerAddr"`              // адрес:порт сервера
	Password    string   `json:"password"`              // пароль подключения
	Hashes      []string `json:"hashes"`                // хеши VK
	DeviceID    string   `json:"deviceId,omitempty"`    // ID устройства
	Workers     int      `json:"workers,omitempty"`     // воркеры (кратно 9)
	CaptchaMode string   `json:"captchaMode,omitempty"` // auto/rjs/wv
	ObfsMode    string   `json:"obfsMode,omitempty"`    // audio/video
	Fingerprint string   `json:"fingerprint,omitempty"` // chrome/android/ios/safari/firefox
	TurnTCP     bool     `json:"turnTcp,omitempty"`     // использовать TCP транспорт
	Listen      string   `json:"listen,omitempty"`      // локальный адрес (порт local из ссылки WDTT-Plus)
	MaxWorkers  int      `json:"maxWorkers,omitempty"`  // предел мощности от владельца сервера (max_workers)
	TurnMode    string   `json:"turnMode,omitempty"`    // «Подключение»: udp или stream (TCP/TLS)
	TurnSNI     string   `json:"turnSni,omitempty"`     // внешний SNI для TURN/TLS
	Exclusions  []string `json:"exclusions,omitempty"`  // сайты/IP/подсети мимо туннеля
}

// ProfileData — данные сервера, хранятся в ~/.config/pwdtt/servers/<name>.json.
type ProfileData struct {
	PeerAddr string   `json:"peer"`              // адрес:порт VPS сервера
	Password string   `json:"password"`          // пароль подключения
	Hashes   []string `json:"hashes"`            // хеши VK-звонков
	Workers  int      `json:"workers,omitempty"` // воркеры (кратно 9)
	Listen   string   `json:"listen"`            // локальный адрес (по умолчанию 127.0.0.1:9000)
	TurnHost string   `json:"turn"`              // переопределение IP TURN
	TurnPort string   `json:"port"`              // переопределение порта TURN
	DeviceID string   `json:"device_id"`         // уникальный ID устройства
	TurnTCP  bool     `json:"turn_tcp"`          // использовать TCP транспорт
	MaxWorkers int    `json:"max_workers,omitempty"` // предел мощности от владельца сервера (WDTT-Plus)
}

// AppSettings — настройки приложения, хранятся в ~/.config/pwdtt/config.json.
type AppSettings struct {
	AutoStart    bool   `json:"autoStart"`    // автозапуск при старте системы
	ObfsMode     string `json:"obfsMode"`     // audio/video
	ObfsAccepted bool   `json:"obfsAccepted"` // пользователь принял предупреждение об обфускации
	TurnTCP      bool   `json:"turnTcp"`      // использовать TCP транспорт
	LinkHandlerOff bool `json:"linkHandlerOff"` // пользователь отключил открытие ссылок wdtt://
}
