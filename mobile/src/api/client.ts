// On a physical device/simulator, "localhost" refers to the device itself,
// not your dev machine — so we derive the dev machine's LAN IP from Metro's
// hostUri (e.g. "192.168.1.4:8081") and use it for the API, unless
// EXPO_PUBLIC_API_URL is explicitly set (or a tunnel such as ngrok is used).
import Constants from "expo-constants";

const hostUri = Constants.expoConfig?.hostUri;
const host = hostUri?.split(":")[0] ?? "localhost";
const API_URL =
  process.env.EXPO_PUBLIC_API_URL || `http://${host}:8080`;

export async function getAyahs(surahId: number) {
  const res = await fetch(`${API_URL}/surahs/${surahId}/ayahs`);
  return res.json();
}

export async function getExplanations(surahId: number, style: string) {
  const res = await fetch(`${API_URL}/surahs/${surahId}/explanations?style=${style}`);
  return res.json();
}

export async function getReciters() {
  const res = await fetch(`${API_URL}/reciters`);
  return res.json();
}

export async function getRecitations(surahId: number, reciterId: string) {
  const res = await fetch(`${API_URL}/surahs/${surahId}/recitations?reciter=${reciterId}`);
  return res.json();
}
