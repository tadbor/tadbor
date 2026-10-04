.PHONY: up up-d down logs seed seed-check dev mobile mobile-web mobile-tunnel mobile-android api-tunnel

# Starts MongoDB (the only Dockerized service) — backend + web are NOT in Docker.
up:
	docker compose up
up-d:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

# One command: MongoDB (Docker) + Go backend (:8080) + Next.js web (:3000),
# all in the foreground — Ctrl+C stops everything. Mobile is separate: make mobile.
#
# The web app needs no API URL of its own: web/next.config.js falls back to
# http://localhost:8080.
dev:
	docker compose up -d && \
	MONGO_URI="mongodb://tadbor:tadbor_dev_password@localhost:27017" go run -C backend ./cmd/api & \
	npm --prefix web run dev & \
	wait

# Loads Surah Yusuf into MongoDB. Run once, after `make up-d`.
#
# The Quran text comes from backend/cmd/seed_quran, which fetches the verified
# Uthmani corpus (issue #2). The reciters still carry placeholder audio (issue #14).
# Both steps upsert, so this is safe to re-run.
#
# The .env is sourced here rather than left to the commands' own godotenv.Load(),
# because those run inside backend/ and the repo's .env is one directory up — which
# godotenv does not search. Sourcing it here also means a developer pointing at
# Atlas gets Atlas instead of a hardcoded localhost.
seed:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	( cd backend && go run ./cmd/seed_quran -surah 12 ) && \
	( cd backend && go run ../scripts/seed_recitation.go )

# Re-verifies the stored corpus against quran.com and three cross-check sources,
# writing nothing. This is the check to run when a verse looks wrong, not a reseed.
seed-check:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	cd backend && go run ./cmd/seed_quran -surah 12 -check

# Mobile runs OUTSIDE Docker (Expo needs to talk to a device/simulator directly —
# see mobile/README.md for why this isn't containerized).
mobile:
	cd mobile && npm install && npm start

# Run the mobile app in the browser instead (react-native-web):
mobile-web:
	cd mobile && npx expo start --web

# Boot the "tadbor" Android emulator (AVD) with a visible phone window, wait for
# it, then start Expo and open the app on it. The emulator reaches your machine
# via 10.0.2.2, so the Go backend (:8080) is hit without any networking tricks.
mobile-android:
	export ANDROID_SDK_ROOT=$$HOME/Android/Sdk ANDROID_HOME=$$HOME/Android/Sdk; \
	if ! $$HOME/Android/Sdk/platform-tools/adb devices | grep -q '^emulator-'; then \
		nohup $$HOME/Android/Sdk/emulator/emulator -avd tadbor -no-snapshot -no-audio -no-boot-anim -gpu swiftshader_indirect > /tmp/emulator.log 2>&1 & \
	fi; \
	$$HOME/Android/Sdk/platform-tools/adb wait-for-device; \
	$$HOME/Android/Sdk/platform-tools/adb shell 'while [ "$$(getprop sys.boot_completed)" != "1" ]; do sleep 2; done'; \
	cd mobile && EXPO_PUBLIC_API_URL=http://10.0.2.2:8080 npx expo start --android --localhost

# Same as `make mobile`, but relays Metro through the cloud — use this when the
# phone and laptop are on different networks (or AP isolation blocks LAN access).
mobile-tunnel:
	cd mobile && npm install && npx expo start --tunnel -c

# Public HTTPS tunnel for the Go backend (:8080). Start this first, copy the
# printed https://xxx.trycloudflare.com URL, then run:
#   EXPO_PUBLIC_API_URL=<that-url> make mobile-tunnel
api-tunnel:
	~/.local/bin/cloudflared tunnel --url http://localhost:8080
