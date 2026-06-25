### Installation
1. Add `-condebug -conclearlog` to your CS2 launch options
2. Download [cs2kz-overlay](https://github.com/YesSeir/cs2kz-overlay/releases)
3. Run `cs2kz-overlay.exe`
4. Add a browser source in OBS pointed at `http://localhost:4433`
5. For progress widget in OBS pointed at `http://localhost:4433/progress`
6. For local gameplay u can use my [cs2kz-pack](https://github.com/YesSeir/cs2kz-pack)
7. For local progress u can use it `http://localhost:4433/progress?type=all&course=main&global=false`

### Parameters

| Parameter | Values (defaults **bold**) | Description |
|-----------|------------------------------|-------------|
| `type`    | **`all`** or `pro` | `all` – counts all your personal bests<br>`pro` – counts only pro personal bests |
| `course`  | **`all`** or `main`, `bonus` | `main` – only the main course of each map<br>`bonus` – only the bonus courses of each map<br>`all` – all courses on each map |
| `global`  | **`true`** or `false` | `true` – uses data from the official [cs2kz-api](https://docs.cs2kz.org)<br>`false` – uses data from `cs2kz.sqlite` and [maps.json](https://raw.githubusercontent.com/YesSeir/cs2kz-maps/main/maps.json) |

The log file located in: `game/csgo/console.log`

Database file located in `game/csgo/addons/cs2kz/data/cs2kz.sqlite3`



