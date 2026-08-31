# idea-lab

Tiny local web app for running visual idea sessions — built for Bertie to
interview Edele about her next side project.

Each "board" is a title, subtitle, bullets, and an AI-generated illustration
(OpenAI `gpt-image-1`). Gallery at `/`, individual boards at `/board/<id>`.
Real-world photo references via `GET /api/photo?q=…` (Openverse, no key).

## Run

```sh
./idea-lab -addr 0.0.0.0:8899
```

Then visit http://<this-host>:8899 from any device on the LAN.

## Create a board (client mode)

```sh
./idea-lab -new "Cellar Tracker" \
  -sub "Drink what you own, before it peaks" \
  -bullets "Scan labels;Drinking-window nudges;Pairs with dinner" \
  -prompt "A cosy wine cellar in soft afternoon light"
```

Note: with Go's flag package, put the `-new` value first — later `flag`
arguments after the first non-flag arg are not parsed.

## Auth

Reads `OPENAI_API_KEY` from the env, or falls back to a `.env` file here
(`OPENAI_API_KEY=…`, chmod 600). The key lives in 1Password vault "Clive"
(item "OpenAI Key").

## Files

- `boards/*.json` — one file per board
- `img/` — generated illustrations
- `INTERVIEW.md` — the idea-session protocol Bertie runs