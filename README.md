<p align="center">
  <img src="assets/icons/icon.png" width="96" />
</p>

<h1 align="center">PWDTT</h1>

<p align="center">
  Десктопный VPN-клиент, который туннелирует трафик через TURN-серверы VK,<br>
  маскируя соединение под зашифрованный медиатрафик звонка.<br>
  <sub>Форк <a href="https://github.com/amurcanov/proxy-turn-vk-android">proxy-turn-vk-android</a> — версия для ПК</sub>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Wails-v2-red?style=for-the-badge&logo=wails&logoColor=white" alt="Wails">
  <img src="https://img.shields.io/badge/Linux-amd64-FCC624?style=for-the-badge&logo=linux&logoColor=black" alt="Linux">
  <img src="https://img.shields.io/badge/Windows-amd64-0078D4?style=for-the-badge&logo=windows&logoColor=white" alt="Windows">
  <img src="https://img.shields.io/badge/macOS-Universal-000000?style=for-the-badge&logo=apple&logoColor=white" alt="macOS">
</p>

---

## Как это работает

Приложение поднимает локальный WireGuard-интерфейс и передаёт его трафик через TURN/DTLS серверы VK, оборачивая пакеты в RTP с шифрованием ChaCha20-Poly1305. С точки зрения провайдера — это обычный зашифрованный VK-звонок.

```
Приложение → WireGuard → ChaCha20/RTP → VK TURN/DTLS → wdtt-server (VPS) → интернет
```

---

## Запуск

### Linux

**1. Установите зависимости:**

```bash
# Ubuntu/Debian
sudo apt install wireguard-tools libwebkit2gtk-4.1-dev

# Arch
sudo pacman -S wireguard-tools webkit2gtk-4.1
```

**2. Настройте права для WireGuard:**

Приложению нужны права root для управления сетевым интерфейсом. Установите sudoers-правило:

```bash
# Одной командой (скачает и запустит):
sudo bash <(curl -s https://raw.githubusercontent.com/luminescq/PWDTT/main/assets/install-sudoers.sh)
```

Или вручную через `visudo` — добавьте в `/etc/sudoers`:
```
your_user ALL=(ALL) NOPASSWD: /usr/bin/ip, /usr/bin/wg
```

Скрипт автоматически определит текущего пользователя и создаст файл `/etc/sudoers.d/pwdtt`.

**3. Запустите:**

```bash
chmod +x pwdtt-linux-amd64
./pwdtt-linux-amd64
```

### Windows

Скачайте `pwdtt-plus-windows-amd64.exe` из [Releases](https://github.com/makdren/PWDTT-Plus/releases) и запустите. Драйвер WireGuard (wintun) встроен.

### macOS

Скачайте `PWDTT-macos.zip` из [Releases](https://github.com/luminescq/PWDTT/releases), распакуйте и запустите `PWDTT.app`. При первом запуске macOS запросит разрешение на создание сетевого интерфейса — введите пароль администратора.

> Universal бинарник: работает на Intel и Apple Silicon (M1/M2/M3/M4).

---

## Быстрый старт

1. **Добавьте сервер** — кнопка `+` → вставьте `wdtt://`-ссылку или введите вручную
2. **VK-хеши** — Настройки → вставьте хеши из `vk.com/call/join/<hash>`
3. **Подключение** — кнопка питания

---

## Ссылки

```
wdtt://<IP>:<DTLS_PORT>:<WG_PORT>:<PROXY_PORT>:<PASSWORD>[:<HASH1>,<HASH2>,...][#название]
```

- Поля 1–5 обязательны
- Хеши — опциональны, через запятую, до 4 штук
- `#название` — опциональный псевдоним сервера

Пример:
```
wdtt://1.2.3.4:56000:56001:0:mypassword:AbCdEfGh,XyZ12345#Мой сервер
```

Вставить ссылку можно через кнопку `+` или просто **Ctrl+V** в любом месте окна.

> Также принимаються ссылки qwdtt.

### Ссылки WDTT Plus

Принимаются ссылки, которые выдаёт [WDTT Plus](https://github.com/Ivan4537/WDTT-Plus) (приложение и бот):

```
wdtt://connect?v=1&host=<HOST>&dtls=<PORT>&wg=<PORT>&local=<PORT>&password=<PASS>&hashes=<H1>,<H2>[&name=<NAME>][&max_workers=<N>]
```

- `host` — IPv4 или домен, `dtls` — порт сервера, `local` — локальный порт клиента
- `hashes` может быть пустым — тогда после импорта откроется профиль, чтобы добавить свои VK-хеши; принимаются и полные ссылки `vk.ru/call/join/...`
- `max_workers` — предел мощности, выставленный владельцем: ползунок «Мощность» не даст его превысить
- старый формат WDTT Plus `wdtt://host:dtls:wg:local:password:hashes` тоже поддерживается
- повторный импорт ссылки с тем же адресом и паролем обновляет существующий профиль, а не создаёт дубль
- если сервер ограничивает число потоков (`POLICY:max_workers=N`), лишние воркеры останавливаются автоматически

В Windows приложение регистрирует себя обработчиком `wdtt://` и `qwdtt://` (HKCU, без прав администратора): клик по ссылке в браузере или Telegram открывает PWDTT Plus и импортирует профиль. Отключается в настройках («Открывать ссылки wdtt://»).

### Обновления

Приложение само проверяет [релизы PWDTT Plus](https://github.com/makdren/PWDTT-Plus/releases) при запуске, а также по кнопке «Проверить обновления» в настройках. «Обновить» скачивает новый exe, сверяет SHA-256, заменяет программу и перезапускает её (VPN при этом отключается). Если что-то пошло не так, остаётся прежняя версия.

Выпуск новой версии: отправьте тег — GitHub Actions соберёт и опубликует релиз сам:

```bash
scripts/release.sh 1.7.1 --watch
```

Скрипт прогоняет тесты, отправляет код и тег `v1.7.1`, а с `--watch` ждёт окончания сборки и показывает ссылку на релиз. Вручную то же самое: `git tag v1.7.1 && git push origin v1.7.1`.

Версия программы берётся из тега. При ручной загрузке релиза: тег вида `v1.7.1` (больше текущей версии), файл `.exe` с `windows` в имени (например `pwdtt-plus-windows-amd64.exe`), релиз не должен быть черновиком или pre-release.

### Скорость с серверами WDTT Plus

С сервером WDTT Plus клиент включает многопоточный режим `WDTT_MUX1` (как Android-приложение WDTT Plus): каждый поток авторизуется на сервере, и входящий трафик раздаётся по всем потокам, а не идёт через один. Сервер определяется автоматически по ответу на запрос конфигурации; со старыми серверами WDTT работает прежний режим.

### Транспорт TURN: Авто / UDP / TCP

В режиме «Авто» (по умолчанию) первые потоки пробуют UDP и TCP поровну, затем выбирается транспорт, который работает стабильнее и быстрее устанавливает соединение (UDP предпочтителен при близких результатах). Потоки на проигравшем транспорте переподключаются. Если выбранный транспорт перестаёт подниматься, клиент переключается на другой; раз в 10 минут выбор перепроверяется.

### Исключения из туннеля

Настройки → «Исключения из туннеля»: сайты, IP или подсети IPv4 по одному на строку — трафик к ним пойдёт напрямую. Домены резолвятся в IP и обновляются каждые 2 минуты; изменения применяются сразу, без переподключения. Поддомены добавляются отдельно (`www.` добавляется автоматически). Если в браузере включён «Безопасный DNS», он может получить другие адреса CDN — в этом случае добавьте подсеть.

Короткие ссылки бота вида `https://<домен>/c/<код>` не поддерживаются: они требуют закрытого API привязки устройства и работают только в Android-приложении WDTT Plus.

---

## Сборка из исходников

**Зависимости:** Go 1.26+, Node.js 22+, [Wails v2](https://wails.io)

```bash
# Linux
sudo apt install libayatana-appindicator3-dev pkg-config gcc libwebkit2gtk-4.1-dev
go install github.com/wailsapp/wails/v2/cmd/wails@latest

git clone https://github.com/luminescq/PWDTT
cd PWDTT
wails build -platform linux/amd64 -tags webkit2_41 -o pwdtt-linux-amd64
# → build/bin/pwdtt-linux-amd64
```

```bash
# Windows (кросс-компиляция с Linux)
wails build -platform windows/amd64
# → build/bin/pwdtt.exe
```

```bash
# macOS (только на macOS)
wails build -platform darwin/universal
# → build/bin/pwdtt-macos
```

---

## Отчёт об ошибках

Если приложение работает некорректно, вы можете собрать отчёт прямо из интерфейса:

1. Откройте **Настройки** (шестерёнка вверху)
2. Нажмите **Отчёт** — информация о системе и логах сессии скопируется в буфер обмена
3. Создайте [Issue](https://github.com/luminescq/PWDTT/issues/new) и вставьте отчёт

Отчёт содержит: версию ОС, версию Go, версию приложения, имя хоста и.filtered логи текущей сессии.

---

> [!IMPORTANT]
> Приложение является техническим инструментом для защищённого туннелирования собственного трафика через ваш сервер. Автор не призывает использовать PWDTT для противоправных целей или нарушения правил сторонних сервисов.

---

## Лицензия

Этот проект распространяется под лицензией GNU General Public License v3.0.
