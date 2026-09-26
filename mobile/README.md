# Tadbor Mobile (React Native / Expo)

**This app is intentionally not part of `docker-compose.yml`.** Expo/React
Native needs to run against an actual simulator, emulator, or physical device
to render anything — a container has no display to render into, so
"dockerizing" it would only get you a container that can run the Metro
bundler, not one you can actually see or interact with. That's not a
limitation worth working around; the standard React Native/Expo workflow
already handles this well.

## Run it

```bash
cd mobile
npm install
npm start        # or: npm run ios / npm run android
```

## Pointing at the backend

Set `EXPO_PUBLIC_API_URL` before starting:

- **iOS Simulator / Android Emulator:** `http://localhost:8080` usually works.
- **Physical device on the same network:** use your machine's LAN IP, e.g.
  `EXPO_PUBLIC_API_URL=http://192.168.1.23:8080 npm start`.
- **No shared network (e.g. testing on the go):** run `npx expo start --tunnel`
  and point `EXPO_PUBLIC_API_URL` at an `ngrok`/similar tunnel to `:8080`.

Make sure `make up` (backend + MongoDB) is running first.
