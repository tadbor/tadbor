import Link from "next/link";
import "./landing.css";
import LandingEmailForm from "./components/LandingEmailForm";

export default function HomePage() {
  return (
    <div className="tl">
      <header className="site-header">
        <div className="container nav">
          <a href="#top" className="logo">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src="/logo.png" alt="Tadbor" className="logo-img" />
            Tadbor
          </a>
          <nav className="nav-links">
            <a href="/surah/12">Quran reader</a>
            <a href="#ai">AI tafsir</a>
            <a href="#privacy">Privacy</a>
          </nav>
          <a href="/surah/12" className="btn btn-primary">
            Open the reader
          </a>
        </div>
      </header>

      <section id="top" className="container hero">
        <div className="hero-grid">
          <div>
            <span className="badge">
              <span className="badge-dot"></span> In development · Surah Yusuf MVP
            </span>
            <h1 className="hero-title">
              Don't just read the Quran.
              <br />
              Understand it.
            </h1>
            <p className="hero-sub">
              Tadbor pairs the Quran's original Arabic text with simplified, tafsir-grounded
              explanations — in classical Arabic, Egyptian dialect, or English — reviewed by a
              human before you ever see it.
            </p>
            <div className="hero-actions">
              <Link href="/surah/12" className="btn btn-primary btn-hero">
                Open the reader
              </Link>
              <a href="#quran" className="btn btn-outline">
                See the reader
              </a>
            </div>
            <div className="hero-meta">
              <span>Tafsir-grounded</span>
              <span>Human reviewed</span>
              <span>Works offline</span>
            </div>
          </div>

          <div className="phone-wrap">
            <div className="iphone">
              <span className="iphone-btn mute"></span>
              <span className="iphone-btn vol-up"></span>
              <span className="iphone-btn vol-down"></span>
              <span className="iphone-btn power"></span>
              <div className="phone-screen">
                <span className="dynamic-island"></span>
                <div className="status-bar">
                  <span>9:41</span>
                  <span className="status-icons">
                    <svg width="16" height="11" viewBox="0 0 16 11" fill="none">
                      <rect x="0" y="7" width="2.6" height="4" rx="0.6" fill="#151515" />
                      <rect x="4.4" y="5" width="2.6" height="6" rx="0.6" fill="#151515" />
                      <rect x="8.8" y="2.5" width="2.6" height="8.5" rx="0.6" fill="#151515" />
                      <rect x="13.2" y="0" width="2.6" height="11" rx="0.6" fill="#151515" opacity="0.35" />
                    </svg>
                    <svg width="21" height="11" viewBox="0 0 21 11" fill="none">
                      <rect x="0.6" y="0.6" width="16.8" height="9.8" rx="2.6" stroke="#151515" strokeOpacity="0.4" />
                      <rect x="2" y="2" width="12" height="6.6" rx="1.3" fill="#151515" />
                      <rect x="18.4" y="3.2" width="1.8" height="4.2" rx="0.9" fill="#151515" opacity="0.4" />
                    </svg>
                  </span>
                </div>
                <div className="app-header">
                  <span className="icon-btn">
                    <svg width="9" height="15" viewBox="0 0 9 15" fill="none">
                      <path d="M8 1L1.5 7.5L8 14" stroke="#151515" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  </span>
                  <div className="app-header-title">
                    <p className="surah-name">Surah Yusuf</p>
                    <p className="surah-meta">Ayah 1 · Simplified Arabic</p>
                  </div>
                  <span className="icon-btn">
                    <svg width="12" height="15" viewBox="0 0 12 15" fill="none">
                      <path d="M1 1H11V14L6 10.5L1 14V1Z" stroke="#151515" strokeWidth="1.6" strokeLinejoin="round" />
                    </svg>
                  </span>
                </div>
                <div className="style-switch">
                  <button className="style-btn active">Simplified</button>
                  <button className="style-btn">Egyptian</button>
                  <button className="style-btn">English</button>
                </div>
                <div className="phone-body">
                  <div className="ayah-tag">
                    <span className="ayah-num">1</span>
                    <span className="ayah-range">Ayat 1–3</span>
                  </div>
                  <div className="quran-card">
                    <p className="arab" style={{ fontSize: 19, textAlign: "right", lineHeight: 1.85, margin: 0 }}>
                      الر ۚ تِلْكَ آيَاتُ الْكِتَابِ الْمُبِينِ
                    </p>
                  </div>
                  <div className="listen-row">
                    <button className="listen-btn">
                      <svg width="8" height="9" viewBox="0 0 8 10" fill="#fff">
                        <path d="M0.8 1.2v7.6c0 .6.7 1 1.2.7l6-3.8c.5-.3.5-1.1 0-1.4l-6-3.8C1.5.2.8.6.8 1.2z" />
                      </svg>
                      Listen
                    </button>
                    <span className="listen-reciter">Mishary Alafasy</span>
                  </div>
                  <div className="explain-card">
                    <div className="explain-head">
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none">
                        <path d="M12 3l1.8 4.6L18.5 9l-4.7 1.4L12 15l-1.8-4.6L5.5 9l4.7-1.4z" fill="#151515" />
                      </svg>
                      <p className="explain-label">Simplified meaning</p>
                    </div>
                    <p className="explain-text">These are the verses of the clear Book.</p>
                    <p className="explain-source">
                      <svg width="10" height="12" viewBox="0 0 14 16" fill="none" stroke="#6B6B6B" strokeWidth="1.6" strokeLinejoin="round">
                        <path d="M2 1h10v14l-5-3.5L2 15z" />
                      </svg>
                      Tafsir Ibn Kathir
                    </p>
                  </div>
                </div>
                <div className="tab-bar">
                  <span className="tab-item active">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#151515" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M4 11l8-7 8 7" />
                      <path d="M6 9.5V21h12V9.5" />
                    </svg>
                    <span className="tab-label">Home</span>
                  </span>
                  <span className="tab-item">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#6B6B6B" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M12 6c-2-1.8-4.5-2.5-8-2.5v15c3.5 0 6 .7 8 2.5 2-1.8 4.5-2.5 8-2.5v-15c-3.5 0-6 .7-8 2.5z" />
                      <path d="M12 6v15" />
                    </svg>
                    <span className="tab-label">Read</span>
                  </span>
                  <span className="tab-item">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#6B6B6B" strokeWidth="2" strokeLinecap="round">
                      <circle cx="11" cy="11" r="7" />
                      <path d="M21 21l-4.5-4.5" />
                    </svg>
                    <span className="tab-label">Search</span>
                  </span>
                  <span className="tab-item">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#6B6B6B" strokeWidth="2" strokeLinecap="round">
                      <circle cx="12" cy="8" r="3.6" />
                      <path d="M5.5 20c1-3.8 3.7-5.8 6.5-5.8s5.5 2 6.5 5.8" />
                    </svg>
                    <span className="tab-label">You</span>
                  </span>
                </div>
                <div className="home-indicator"></div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="strip">
        <div className="container strip-inner">
          <span>Uthmani script</span>
          <span>Tafsir-grounded AI</span>
          <span>Dialect adaptation</span>
          <span>4 reciters</span>
          <span>Human review</span>
          <span>Full citations</span>
          <span>Offline reading</span>
        </div>
      </section>

      <section id="quran" className="container section">
        <p className="eyebrow">How it works</p>
        <h2 className="section-title">Built to explain, not to interpret.</h2>
        <p className="section-sub">
          Every explanation traces back to an approved source, and nothing publishes without a
          human reviewer's sign-off.
        </p>
        <div className="grid-cards">
          <div className="card">
            <div className="card-icon">Q</div>
            <h3>Immutable Quran text</h3>
            <p>The Uthmani script, never rewritten or paraphrased by AI — kept structurally separate from every explanation.</p>
          </div>
          <div className="card">
            <div className="card-icon">T</div>
            <h3>Tafsir-grounded explanations</h3>
            <p>AI only simplifies and translates existing, approved tafsir — it never invents a religious interpretation.</p>
          </div>
          <div className="card">
            <div className="card-icon">D</div>
            <h3>Dialect adaptation</h3>
            <p>The same reviewed meaning, expressed in Simplified Arabic, Egyptian Arabic, or English.</p>
          </div>
          <div className="card">
            <div className="card-icon">R</div>
            <h3>Mandatory human review</h3>
            <p>Nothing reaches a reader until a qualified reviewer checks it for accuracy against the source.</p>
          </div>
          <div className="card">
            <div className="card-icon">C</div>
            <h3>Full citations</h3>
            <p>Every explanation shows exactly which tafsir source it came from — nothing is unattributed.</p>
          </div>
          <div className="card">
            <div className="card-icon">O</div>
            <h3>Reads offline</h3>
            <p>Once loaded, the Quran text and its published explanations are available without a connection.</p>
          </div>
          <div className="card">
            <div className="card-icon">L</div>
            <h3>4 reciters</h3>
            <p>Listen to each ayah recited by Mishary Alafasy, Abdul Basit, Al-Ghamdi, or Al-Muaiqly — switch reciters anytime.</p>
          </div>
        </div>
      </section>

      <section id="ai" className="ai-section">
        <div className="container ai-grid">
          <div className="chat-card">
            <p className="chat-context">Context: Surah Yusuf, ayah 4</p>
            <div className="chat-user">
              <div className="chat-bubble-user">Why does Yusuf tell his father about the dream?</div>
            </div>
            <div className="chat-bubble-ai">
              <p>The commentary explains this as the beginning of a pattern of trust and eventual fulfillment running through the surah — see the cited source for the full explanation.</p>
              <span className="chat-tag">TAFSIR IBN KATHIR</span>
            </div>
          </div>
          <div>
            <p className="eyebrow" style={{ color: "rgba(255,255,255,0.6)" }}>
              AI tafsir
            </p>
            <h2 className="ai-title">Ask about any ayah. Answered with sources.</h2>
            <p className="ai-sub">
              The AI in Tadbor only transforms evidence it's given — it never fills gaps with its
              own interpretation.
            </p>
            <div className="ai-points">
              <p>
                <strong>No invented tafsir.</strong> Every claim traces to a source, and scholarly
                disagreement is preserved, not smoothed over.
              </p>
              <p>
                <strong>No unsupported answers.</strong> If there's no approved evidence for a
                verse, Tadbor says so instead of guessing.
              </p>
            </div>
          </div>
        </div>
      </section>

      <section id="privacy" className="container section">
        <p className="eyebrow">Privacy</p>
        <h2 className="section-title">Reading needs no account.</h2>
        <div className="grid-cards" style={{ marginTop: 40 }}>
          <div className="card">
            <h3>No login to read</h3>
            <p>The Quran, its explanations, and citations are open the moment you open the app.</p>
          </div>
          <div className="card">
            <h3>No ads, no trackers</h3>
            <p>Nothing about how you read is collected or sold.</p>
          </div>
          <div className="card">
            <h3>Reviewed, not generic</h3>
            <p>Every explanation you read passed a human reviewer before publishing.</p>
          </div>
        </div>
      </section>

      <section id="early-access" className="early">
        <div className="early-inner">
          <h2>Be among the first to read with Tadbor.</h2>
          <p>Surah Yusuf launches first. Leave your email and we'll let you know.</p>
          <LandingEmailForm />
        </div>
      </section>

      <footer className="site-footer">
        <div className="container">
          <div className="footer-grid">
            <div className="footer-brand">
              <a href="#top" className="footer-logo">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src="/logo_for_dark.png" alt="Tadbor" className="footer-logo-img" />
                Tadbor
              </a>
              <p className="footer-tagline">
                The Quran, understood. Tafsir-grounded explanations, human reviewed, rooted in
                scholarly sources.
              </p>
              <div className="footer-social">
                <a href="https://github.com/" aria-label="GitHub">
                  <svg width="17" height="17" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M12 .5A11.5 11.5 0 0 0 .5 12a11.5 11.5 0 0 0 7.86 10.92c.58.1.79-.25.79-.56v-2c-3.2.7-3.87-1.37-3.87-1.37-.53-1.33-1.28-1.69-1.28-1.69-1.05-.71.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.76 2.7 1.25 3.36.95.1-.75.4-1.25.73-1.54-2.55-.29-5.23-1.28-5.23-5.68 0-1.25.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.04 0 0 .97-.31 3.18 1.18a11.1 11.1 0 0 1 5.8 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.58.23 2.75.11 3.04.74.81 1.19 1.84 1.19 3.09 0 4.41-2.69 5.38-5.25 5.67.4.35.77 1.04.77 2.1v3.12c0 .3.2.67.8.56A11.5 11.5 0 0 0 23.5 12 11.5 11.5 0 0 0 12 .5z" />
                  </svg>
                </a>
                <a href="https://x.com/" aria-label="X">
                  <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
                  </svg>
                </a>
                <a href="https://www.linkedin.com/" aria-label="LinkedIn">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M20.45 20.45h-3.56v-5.57c0-1.33-.02-3.04-1.85-3.04-1.85 0-2.14 1.45-2.14 2.94v5.67H9.35V9h3.41v1.56h.05a3.74 3.74 0 0 1 3.37-1.85c3.6 0 4.27 2.37 4.27 5.46zM5.34 7.43a2.06 2.06 0 1 1 0-4.13 2.06 2.06 0 0 1 0 4.13zM7.12 20.45H3.56V9h3.56zM22.22 0H1.77C.79 0 0 .77 0 1.72v20.56C0 23.23.79 24 1.77 24h20.45c.98 0 1.78-.77 1.78-1.72V1.72C24 .77 23.2 0 22.22 0z" />
                  </svg>
                </a>
                <a href="https://www.youtube.com/" aria-label="YouTube">
                  <svg width="17" height="17" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M23.5 6.19a3.02 3.02 0 0 0-2.12-2.14C19.5 3.55 12 3.55 12 3.55s-7.5 0-9.38.5A3.02 3.02 0 0 0 .5 6.19C0 8.07 0 12 0 12s0 3.93.5 5.81a3.02 3.02 0 0 0 2.12 2.14c1.88.5 9.38.5 9.38.5s7.5 0 9.38-.5a3.02 3.02 0 0 0 2.12-2.14C24 15.93 24 12 24 12s0-3.93-.5-5.81zM9.55 15.57V8.43L15.82 12z" />
                  </svg>
                </a>
              </div>
            </div>

            <div className="footer-col">
              <h4>Product</h4>
              <ul>
                <li><a href="/surah/12">Quran reader</a></li>
                <li><a href="#ai">AI tafsir</a></li>
                <li><a href="#quran">How it works</a></li>
                <li><a href="#early-access">Early access</a></li>
              </ul>
            </div>

            <div className="footer-col">
              <h4>Developers</h4>
              <ul>
                <li><a href="https://github.com/">Repository</a></li>
                <li><a href="https://github.com/">Issues</a></li>
                <li><a href="http://localhost:8080">Backend API</a></li>
                <li><a href="http://localhost:8080/health">Health check</a></li>
              </ul>
            </div>

            <div className="footer-col">
              <h4>Company</h4>
              <ul>
                <li><a href="#top">About</a></li>
                <li><a href="#privacy">Privacy</a></li>
                <li><a href="#top">Contact</a></li>
                <li><a href="#top">Terms</a></li>
              </ul>
            </div>
          </div>

          <div className="footer-bottom">
            <p>© 2026 Tadbor. A companion, not a scholar — for questions of religious ruling, consult a qualified scholar.</p>
            <p>Built with tafsir sources and human reviewers.</p>
          </div>
        </div>
      </footer>
    </div>
  );
}