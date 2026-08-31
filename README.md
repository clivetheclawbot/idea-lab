# idea-lab

<p align="center">
  <img src="docs/hero.jpg" alt="Pinned idea cards on a wall in warm afternoon light — phone parked in a mug, a large black cat asleep on the desk, nothing urgent" width="720">
</p>

Tiny local web app for running visual idea sessions — built for an agent to
interview a human about their next side project.

Each "board" is a title, subtitle, bullets, and an AI-generated illustration
(OpenAI `gpt-image-1`). Gallery at `/`, individual boards at `/board/<id>`.
Real-world photo references via `GET /api/photo?q=…` (Openverse, no key).

## Run

It's a systemd user service (enabled at boot, restarts on failure):

```sh
systemctl --user status idea-lab     # check
systemctl --user restart idea-lab    # nudge
journalctl --user -u idea-lab -f     # logs
```

Then visit http://<this-host>:8899 from any device on the LAN.

## Client commands

```sh
./idea-lab new "Title" -sub "…" -bullets "a;b;c" -prompt "…" [-imgurl URL]
./idea-lab edit <id-suffix> [-title …] [-sub …] [-bullets …] [-prompt …] [-imgpath /img/x.png]
./idea-lab ls
```

- `new` generates an illustration (gpt-image-1, ~30–60 s).
- `edit` is instant when `-prompt` is unchanged (keeps the existing image);
  a changed prompt regenerates. `-imgpath` forces a specific image.
- Flags and positionals can be interleaved; quote values with spaces.

## Auth

Reads `OPENAI_API_KEY` from the env, or falls back to a `.env` file here
(`OPENAI_API_KEY=…`, chmod 600).

## Files

- `boards/*.json` — one file per board
- `img/` — generated illustrations
- `INTERVIEW.md` — the idea-session protocol Bertie runs