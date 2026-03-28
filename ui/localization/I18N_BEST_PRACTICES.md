# JetKVM i18n Best Practices

Research and guidelines for localizing a KVM-over-IP appliance web UI.

## Product Context

JetKVM is a hardware KVM-over-IP device. Its web UI is used by IT professionals, sysadmins, and homelab enthusiasts to remotely control computers. The tone is **technical but accessible, modern, and concise** — like a well-made developer tool, not enterprise middleware.

## Universal Rules

1. **Keep technical terms in English** when they are industry-standard and universally recognized in IT:
   - HDMI, USB, SSH, TLS, HTTPS, DHCP, DNS, NTP, MQTT, LLDP, mDNS, EDID, WebRTC, DTLS, SRTP, OIDC, IPv4, IPv6, ATX, MAC, ICE, SLAAC, DHCPv6, CIDR, MTU, TTL, CRLF, ANSI, OCR, Wake on LAN, loopback, jitter
   - Brand names: JetKVM, GitHub, Tailscale, Home Assistant, Cloudflare, Grafana

2. **Keep UI element names short.** Buttons, labels, and menu items must fit constrained spaces. Prefer terse phrasing over full sentences.

3. **Preserve `{placeholder}` tokens exactly.** Never translate, reorder, or add spaces inside `{error}`, `{name}`, `{version}`, etc.

4. **Match formality level to locale conventions** for tech products (see per-language notes).

5. **Don't over-translate.** If a term (e.g., "Wake on LAN", "loopback", "firmware") has no widely-adopted native equivalent in the target language's tech community, keep the English term.

6. **Consistency within each language.** Pick one word for each concept and stick to it across all 1000+ keys. E.g., "device" should always map to the same word, not alternate between synonyms.

---

## Per-Language Guidelines

### English (en) — Base

- Reference file. All other languages must have every key present in `en.json`.
- Tone: direct, no unnecessary words, no jargon that isn't KVM/IT-specific.

### German (de)

- **Formality:** Use "Sie" (formal you). Standard for German tech UIs.
- **Terminology:** Use "Passwort" consistently (not "Kennwort" — "Passwort" is the dominant term in modern German tech). Use "Gerät" for device, "Einstellungen" for settings.
- **Compound nouns:** German allows them freely — use them (e.g., "Zugriffskontrolle", "Netzwerkeinstellungen") but don't force overly long compounds.
- **Keep English:** loopback, Wake on LAN, firmware, SSH, MQTT, jitter, streaming. "Dev Channel" can stay.
- **Common pitfall:** Don't translate "Cloud" — "Cloud" is standard in German IT.

### French (fr)

- **Formality:** Use "vous" (formal). Standard in French tech/product UIs.
- **Terminology:** Use "chiffré" (not "crypté") for encrypted — "chiffré" is the technically correct French term per ANSSI. Use "mot de passe" for password. Use "appareil" for device.
- **Nav items/titles:** Use nouns, not verbs. E.g., "Accès" (not "Accéder") for an access settings page title.
- **Keep English:** loopback, Wake on LAN, firmware, streaming, Cloud. Don't translate "mode loopback" to "mode de bouclage".
- **Typography:** Use non-breaking space before `:`, `!`, `?`, `;` (French typographic convention). The translation framework may not support this — use regular space if needed but be aware.

### Spanish (es)

- **Formality:** Use "usted" form. Standard for tech products in Spanish.
- **Terminology:** Use "contraseña" for password, "dispositivo" for device, "configuración" for settings/configuration.
- **Keep English:** Wake on LAN (not "Activación en LAN"), loopback (not "bucle invertido"), firmware, streaming, Cloud.
- **Regional neutrality:** Use neutral Latin American/Iberian Spanish — avoid region-specific slang. "Computadora" and "ordenador" are both fine but pick one and be consistent.

### Italian (it)

- **Formality:** Can use informal "tu" or formal "Lei" — modern Italian tech UIs increasingly use imperative form or impersonal constructions. Stay consistent.
- **Terminology:** Use "password" (borrowed into Italian IT) or "password" consistently — not alternating with "parola d'ordine". Use "dispositivo" for device.
- **Keep English:** loopback, Wake on LAN, firmware, streaming, Cloud.
- **Buttons:** Use infinitive form ("Aggiorna", "Configura") — standard Italian UI convention.

### Japanese (ja)

- **Formality:** Use です/ます (polite form) for descriptions. Use plain noun form for labels/buttons.
- **Katakana:** Use katakana for established loanwords: パスワード, デバイス, ストリーミング, ファームウェア, ネットワーク.
- **Keep English:** Technical acronyms stay in ASCII (SSH, TLS, MQTT, HDMI, USB, etc.). "JetKVM" stays as-is.
- **Length:** Japanese is typically 30-50% shorter than English. Take advantage of this for concise labels.
- **Particles:** Be precise with particles — が vs は matters for nuance.

### Russian (ru)

- **Formality:** Use "вы" (formal you, lowercase) in instructions. Standard for Russian tech UIs.
- **Terminology:** Use "пароль" for password, "устройство" for device, "настройки" for settings.
- **Transliteration:** Use established transliterations — "файрвол" not "брандмауэр" for firewall (if used), "стриминг" for streaming.
- **Keep English:** SSH, TLS, MQTT, Wake on LAN, loopback, EDID, Cloud (or "облако" — both are used, pick one and be consistent).
- **Cases:** Pay attention to grammatical cases in error messages with placeholders.

### Chinese Simplified (zh)

- **Terminology:** Use mainland China standard terminology: "密码" for password, "设备" for device, "设置" for settings, "网络" for network.
- **Keep English:** Technical acronyms (SSH, TLS, MQTT, etc.) stay in English. "JetKVM" stays as-is.
- **Conciseness:** Chinese naturally compresses well. Don't pad with unnecessary words.
- **Punctuation:** Use Chinese punctuation (。，：) for full sentences, but English punctuation for labels/short phrases in UI context.

### Chinese Traditional (zh-tw)

- **Terminology:** Use Taiwan standard terminology where it differs from mainland: "韌體" (not "固件") for firmware, "裝置" (not "设备") for device, but "密碼" for password.
- **Script:** Ensure all characters are Traditional, not Simplified. This is a common machine-translation error.
- **Tone:** Taiwan tech writing tends to be slightly more formal than mainland.

### Portuguese (pt)

- **Variant:** European Portuguese (pt-PT), not Brazilian Portuguese. Key differences:
  - "ecrã" (not "tela") for screen
  - "ficheiro" (not "arquivo") for file
  - "descarregar" (not "baixar") for download
  - "definições" or "configurações" for settings
- **Formality:** Use "você" or 3rd person formal constructions.
- **Keep English:** SSH, TLS, MQTT, Wake on LAN, loopback, Cloud, firmware.

### Swedish (sv)

- **Formality:** Use "du" (informal). Swedish tech UIs universally use "du"-tilltal.
- **Terminology:** Use "lösenord" for password, "enhet" for device, "inställningar" for settings.
- **Key fixes:** "Tillägg" (not "Förlängning") for software extension. "Åtkomst" (not "Tillträde") for access in IT context. Keep "Wake on LAN" (not "Vakna på LAN").
- **Keep English:** loopback, Wake on LAN, firmware, streaming, Cloud (or "molnet" — Swedish often uses "molnet").

### Norwegian Bokmål (nb)

- **Formality:** Use informal tone. Norwegian tech UIs are informal.
- **Terminology:** Use "passord" for password, "enhet" for device, "innstillinger" for settings.
- **Key fixes:** "Utvidelse" (not "Forlengelse") for software extension. Keep "Wake on LAN" (not "Vekk på LAN").
- **Keep English:** loopback, Wake on LAN, firmware, streaming, Cloud (or "skyen").

### Danish (da)

- **Formality:** Use "du" (informal). Standard for Danish tech.
- **Terminology:** Use "adgangskode" for password, "enhed" for device, "indstillinger" for settings.
- **Key fixes:** Keep "Wake on LAN" (not "Vågn på LAN"). "Udvidelse" for extension is correct.
- **Keep English:** loopback, Wake on LAN, firmware, streaming, Cloud.

### Welsh (cy)

- **Formality:** Use "chi" (formal/plural you) — standard for Welsh software localization.
- **Terminology:** Welsh has established tech terminology from Termiadur Addysg / Microsoft Welsh language packs. Use "cyfrinair" for password, "dyfais" for device, "gosodiadau" for settings.
- **Keep English:** Technical terms that have no established Welsh equivalent: SSH, TLS, MQTT, HDMI, USB, EDID, loopback, Wake on LAN, Cloud (or "Cwmwl").
- **Note:** Welsh has initial consonant mutation — ensure mutations are correct after prepositions and in compound phrases.

---

## Structural Requirements

- Every language file must contain exactly the same keys as `en.json`.
- Remove any keys that no longer exist in `en.json` (e.g., old `advanced_reset_config_*` keys).
- Keys are sorted alphabetically.
- All files must pass `inlang validate`.
