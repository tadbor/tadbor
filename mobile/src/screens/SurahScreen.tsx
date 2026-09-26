import React, { useEffect, useRef, useState } from "react";
import { ScrollView, View, Text, StyleSheet, Pressable } from "react-native";
import { createAudioPlayer, setAudioModeAsync } from "expo-audio";
import { getAyahs, getExplanations, getReciters, getRecitations } from "../api/client";

const STYLES = [
  { key: "simplified_ar", label: "Simplified" },
  { key: "egyptian", label: "Egyptian" },
  { key: "english", label: "English" },
];

const TAB_ITEMS = [
  { key: "home", label: "Home", icon: "◱" },
  { key: "read", label: "Read", icon: "📖" },
  { key: "search", label: "Search", icon: "🔍" },
  { key: "you", label: "You", icon: "👤" },
];

export default function SurahScreen({ surahId = 12 }: { surahId?: number }) {
  const [ayahs, setAyahs] = useState<any[]>([]);
  const [explanations, setExplanations] = useState<Record<number, any>>({});
  const [reciters, setReciters] = useState<any[]>([]);
  const [activeReciter, setActiveReciter] = useState<string | null>(null);
  const [activeStyle, setActiveStyle] = useState("simplified_ar");
  const [audioByAyah, setAudioByAyah] = useState<Record<number, any>>({});
  const [playingAyah, setPlayingAyah] = useState<number | null>(null);
  const playerRef = useRef<ReturnType<typeof createAudioPlayer> | null>(null);

  useEffect(() => {
    setAudioModeAsync({ playsInSilentMode: true });
  }, []);

  useEffect(() => {
    getAyahs(surahId).then(setAyahs);
    getExplanations(surahId, activeStyle).then((list: any[]) => {
      const byAyah: Record<number, any> = {};
      (list || []).forEach((e) => (byAyah[e.ayah_start] = e));
      setExplanations(byAyah);
    });
    getReciters().then((list: any[]) => {
      setReciters(list || []);
      if (list?.length) setActiveReciter(list[0].id);
    });
  }, [surahId]);

  useEffect(() => {
    getExplanations(surahId, activeStyle).then((list: any[]) => {
      const byAyah: Record<number, any> = {};
      (list || []).forEach((e) => (byAyah[e.ayah_start] = e));
      setExplanations(byAyah);
    });
  }, [surahId, activeStyle]);

  useEffect(() => {
    if (!activeReciter) return;
    getRecitations(surahId, activeReciter).then((list: any[]) => {
      const byAyah: Record<number, any> = {};
      (list || []).forEach((r) => (byAyah[r.ayah_number] = r));
      setAudioByAyah(byAyah);
    });
  }, [surahId, activeReciter]);

  async function playAyah(ayahNumber: number, url: string) {
    if (playerRef.current) {
      playerRef.current.pause();
      playerRef.current.release();
      playerRef.current = null;
    }
    const player = createAudioPlayer({ uri: url }, { updateInterval: 100 });
    playerRef.current = player;

    const subscription = player.addListener("playbackStatusUpdate", (status: any) => {
      if (status.didJustFinish) {
        setPlayingAyah(null);
        subscription.remove();
      }
    });
    setPlayingAyah(ayahNumber);
    player.play();
  }

  const activeReciterName =
    reciters.find((r) => r.id === activeReciter)?.name ?? "";

  return (
    <View style={styles.container}>
      <View style={styles.appHeader}>
        <Text style={styles.backChevron}>‹</Text>
        <View style={styles.headerCenter}>
          <Text style={styles.surahName}>Surah Yusuf</Text>
          <Text style={styles.surahMeta}>
            Ayah 1 · {STYLES.find((s) => s.key === activeStyle)?.label} Arabic
          </Text>
        </View>
        <Text style={styles.bookmark}>🔖</Text>
      </View>

      <View style={styles.styleSwitch}>
        {STYLES.map((s) => (
          <Pressable
            key={s.key}
            onPress={() => setActiveStyle(s.key)}
            style={[styles.styleBtn, s.key === activeStyle && styles.styleBtnActive]}
          >
            <Text
              style={[
                styles.styleBtnText,
                s.key === activeStyle && styles.styleBtnTextActive,
              ]}
            >
              {s.label}
            </Text>
          </Pressable>
        ))}
      </View>

      <ScrollView style={styles.body} contentContainerStyle={styles.bodyContent}>
        {ayahs.map((ayah) => {
          const audio = audioByAyah[ayah.ayah_number];
          const explanation = explanations[ayah.ayah_number];
          return (
            <View key={ayah.ayah_number} style={styles.ayahSection}>
              <View style={styles.ayahTag}>
                <View style={styles.ayahNum}>
                  <Text style={styles.ayahNumText}>{ayah.ayah_number}</Text>
                </View>
                <Text style={styles.ayahRange}>Ayat {ayah.ayah_number}</Text>
              </View>

              <View style={styles.quranCard}>
                <Text style={styles.quranText}>{ayah.text_uthmani}</Text>
              </View>

              {audio && (
                <View style={styles.listenRow}>
                  <Pressable
                    onPress={() => playAyah(ayah.ayah_number, audio.audio_url)}
                    style={styles.listenBtn}
                  >
                    <Text style={styles.listenBtnText}>
                      {playingAyah === ayah.ayah_number ? "⏸" : "▶"}{" "}
                      {playingAyah === ayah.ayah_number ? "Playing…" : "Listen"}
                    </Text>
                  </Pressable>
                  <Text style={styles.listenReciter} numberOfLines={1}>
                    {activeReciterName}
                  </Text>
                </View>
              )}

              {explanation && (
                <View style={styles.explainCard}>
                  <Text style={styles.explainLabel}>💡 Simplified meaning</Text>
                  <Text style={styles.explainText}>{explanation.text}</Text>
                  <Text style={styles.explainSource}>📕 Tafsir Ibn Kathir</Text>
                </View>
              )}
            </View>
          );
        })}
      </ScrollView>

      <View style={styles.tabBar}>
        {TAB_ITEMS.map((tab, i) => (
          <View key={tab.key} style={styles.tabItem}>
            <Text style={[styles.tabIcon, i === 0 && styles.tabIconActive]}>{tab.icon}</Text>
            <Text style={[styles.tabLabel, i === 0 && styles.tabLabelActive]}>{tab.label}</Text>
          </View>
        ))}
      </View>
      <View style={styles.homeIndicator} />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: "#FFFFFF" },
  appHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 12,
    paddingTop: 8,
  },
  backChevron: { fontSize: 30, color: "#151515", paddingHorizontal: 4 },
  bookmark: { fontSize: 18, paddingHorizontal: 8 },
  headerCenter: { alignItems: "center" },
  surahName: { fontSize: 16, fontWeight: "800", color: "#151515" },
  surahMeta: { fontSize: 11, color: "#6B6B6B", fontWeight: "600", marginTop: 2 },

  styleSwitch: {
    flexDirection: "row",
    marginHorizontal: 16,
    marginTop: 10,
    padding: 4,
    borderRadius: 999,
    backgroundColor: "#F7F7F7",
  },
  styleBtn: {
    flex: 1,
    alignItems: "center",
    paddingVertical: 7,
    borderRadius: 999,
  },
  styleBtnActive: { backgroundColor: "#151515" },
  styleBtnText: { fontSize: 12, fontWeight: "700", color: "#6B6B6B" },
  styleBtnTextActive: { color: "#FFFFFF" },

  body: { flex: 1 },
  bodyContent: { paddingHorizontal: 16, paddingTop: 14, paddingBottom: 16 },
  ayahSection: { marginBottom: 20 },
  ayahTag: { flexDirection: "row", alignItems: "center", gap: 6, marginBottom: 8 },
  ayahNum: {
    width: 22,
    height: 22,
    borderRadius: 11,
    backgroundColor: "#1515151A",
    alignItems: "center",
    justifyContent: "center",
  },
  ayahNumText: { fontSize: 11, fontWeight: "800", color: "#151515" },
  ayahRange: {
    fontSize: 10,
    fontWeight: "700",
    letterSpacing: 1,
    textTransform: "uppercase",
    color: "#6B6B6B",
  },

  quranCard: {
    borderWidth: 1,
    borderColor: "#1515151A",
    borderRadius: 16,
    padding: 18,
    backgroundColor: "#FFFFFF",
  },
  quranText: {
    fontSize: 20,
    lineHeight: 36,
    textAlign: "right",
    fontFamily: "serif",
  },

  listenRow: { flexDirection: "row", alignItems: "center", gap: 10, marginTop: 10 },
  listenBtn: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    backgroundColor: "#151515",
    borderRadius: 999,
    paddingVertical: 6,
    paddingHorizontal: 14,
  },
  listenBtnText: { fontSize: 12, fontWeight: "700", color: "#FFFFFF" },
  listenReciter: { fontSize: 10, fontWeight: "600", color: "#6B6B6B", flexShrink: 1 },

  explainCard: {
    backgroundColor: "#F7F7F7",
    borderRadius: 16,
    padding: 16,
    marginTop: 10,
  },
  explainLabel: { fontSize: 12, fontWeight: "800", color: "#151515" },
  explainText: { fontSize: 13, lineHeight: 21, marginTop: 8, color: "#3a3a3a" },
  explainSource: { fontSize: 10, fontWeight: "600", color: "#6B6B6B", marginTop: 10 },

  tabBar: {
    flexDirection: "row",
    justifyContent: "space-around",
    paddingVertical: 10,
    borderTopWidth: 1,
    borderTopColor: "#1515151A",
    backgroundColor: "#FFFFFF",
  },
  tabItem: { alignItems: "center", gap: 3 },
  tabIcon: { fontSize: 18, opacity: 0.55 },
  tabIconActive: { opacity: 1 },
  tabLabel: { fontSize: 9, fontWeight: "700", color: "#6B6B6B" },
  tabLabelActive: { color: "#151515" },
  homeIndicator: {
    width: 120,
    height: 4,
    borderRadius: 2,
    backgroundColor: "#151515",
    opacity: 0.85,
    alignSelf: "center",
    marginVertical: 6,
  },
});