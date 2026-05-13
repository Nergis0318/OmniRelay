<template>
  <v-app>
    <router-view />
  </v-app>
</template>

<style>
/* ── Reset & Base ── */
*,
*::before,
*::after {
  box-sizing: border-box;
}

:root {
  --clr-bg: #0d0d0f;
  --clr-surface: #131316;
  --clr-surface-2: #1a1a1f;
  --clr-surface-3: #222228;
  --clr-border: rgba(255, 255, 255, 0.07);
  --clr-border-accent: rgba(232, 160, 32, 0.35);
  --clr-amber: #e8a020;
  --clr-amber-bright: #f5c842;
  --clr-teal: #2ec4b6;
  --clr-text: #e8e6e1;
  --clr-text-muted: #7c7a75;
  --clr-text-dim: #4a4844;
  --font-display: "Fraunces", Georgia, serif;
  --font-body: "DM Sans", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", "Cascadia Code", monospace;
}

html,
body {
  background: var(--clr-bg) !important;
  color: var(--clr-text) !important;
  font-family: var(--font-body) !important;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

/* ── Scrollbar ── */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
::-webkit-scrollbar-track {
  background: transparent;
}
::-webkit-scrollbar-thumb {
  background: var(--clr-surface-3);
  border-radius: 3px;
}
::-webkit-scrollbar-thumb:hover {
  background: var(--clr-amber);
}

/* ── Typography ── */
.v-application {
  font-family: var(--font-body) !important;
  background: var(--clr-bg) !important;
}

h1,
h2,
h3,
.page-title {
  font-family: var(--font-display) !important;
  font-weight: 600;
  letter-spacing: -0.02em;
  line-height: 1.15;
  color: var(--clr-text);
}

.text-h4 {
  font-family: var(--font-display) !important;
  font-size: 1.6rem !important;
  font-weight: 600 !important;
  letter-spacing: -0.025em !important;
}

code,
.mono,
pre {
  font-family: var(--font-mono) !important;
  font-size: 0.85em;
  background: var(--clr-surface-2);
  border: 1px solid var(--clr-border);
  border-radius: 4px;
  padding: 0.1em 0.45em;
  color: var(--clr-amber);
}

/* ── Vuetify Surface Overrides ── */
.v-card {
  background: var(--clr-surface) !important;
  border: 1px solid var(--clr-border) !important;
  color: var(--clr-text) !important;
}

.v-card-title {
  font-family: var(--font-display) !important;
  font-size: 1.05rem !important;
  font-weight: 600 !important;
  letter-spacing: -0.01em !important;
  padding-top: 20px !important;
  padding-bottom: 8px !important;
  color: var(--clr-text) !important;
}

.v-list,
.v-navigation-drawer {
  background: var(--clr-surface) !important;
  border-right: 1px solid var(--clr-border) !important;
}

.v-main {
  background: var(--clr-bg) !important;
}

.v-container {
  background: transparent !important;
}

/* ── Form Fields ── */
.v-field {
  background: var(--clr-surface-2) !important;
  border-color: var(--clr-border) !important;
}
.v-field__outline {
  --v-field-border-color: var(--clr-border) !important;
  --v-field-border-opacity: 1 !important;
}
.v-field--focused .v-field__outline {
  --v-field-border-color: var(--clr-amber) !important;
  box-shadow: 0 0 0 1px rgba(232, 160, 32, 0.2) !important;
}
.v-label {
  color: var(--clr-text-muted) !important;
  font-family: var(--font-body) !important;
  font-size: 0.85rem !important;
}
.v-field--focused .v-label {
  color: var(--clr-amber) !important;
}
.v-field__input {
  color: var(--clr-text) !important;
  font-family: var(--font-body) !important;
}

/* ── Buttons ── */
.v-btn {
  font-family: var(--font-body) !important;
  font-weight: 500 !important;
  letter-spacing: 0.02em !important;
  text-transform: none !important;
}
.v-btn--variant-elevated.v-btn--color-primary,
.v-btn[class*="bg-primary"] {
  background: var(--clr-amber) !important;
  color: #0d0d0f !important;
  box-shadow: 0 0 16px rgba(232, 160, 32, 0.25) !important;
}
.v-btn--variant-elevated.v-btn--color-primary:hover,
.v-btn[class*="bg-primary"]:hover {
  background: var(--clr-amber-bright) !important;
  box-shadow: 0 0 24px rgba(232, 160, 32, 0.4) !important;
}
.v-btn--color-error {
  color: #ff5757 !important;
}

/* ── Chips ── */
.v-chip {
  font-family: var(--font-mono) !important;
  font-size: 0.72rem !important;
  letter-spacing: 0.04em !important;
  font-weight: 500 !important;
}
.v-chip--color-success {
  background: rgba(46, 196, 182, 0.12) !important;
  color: var(--clr-teal) !important;
  border: 1px solid rgba(46, 196, 182, 0.3) !important;
}
.v-chip--color-error {
  background: rgba(255, 87, 87, 0.1) !important;
  color: #ff5757 !important;
  border: 1px solid rgba(255, 87, 87, 0.25) !important;
}
.v-chip--color-warning {
  background: rgba(232, 160, 32, 0.12) !important;
  color: var(--clr-amber) !important;
  border: 1px solid rgba(232, 160, 32, 0.3) !important;
}

/* ── Tables ── */
.v-data-table {
  background: var(--clr-surface) !important;
  border: 1px solid var(--clr-border) !important;
}
.v-data-table__th {
  font-family: var(--font-mono) !important;
  font-size: 0.7rem !important;
  letter-spacing: 0.1em !important;
  text-transform: uppercase !important;
  color: var(--clr-text-muted) !important;
  border-bottom: 1px solid var(--clr-border) !important;
  background: var(--clr-surface) !important;
}
.v-data-table__tr {
  border-bottom: 1px solid var(--clr-border) !important;
  transition: background 0.12s ease !important;
}
.v-data-table__tr:hover td {
  background: rgba(232, 160, 32, 0.04) !important;
}
.v-data-table__td {
  font-family: var(--font-body) !important;
  font-size: 0.875rem !important;
  color: var(--clr-text) !important;
  border-bottom: 1px solid var(--clr-border) !important;
}

/* ── Dialogs ── */
.v-dialog .v-overlay__content .v-card {
  border: 1px solid var(--clr-border-accent) !important;
  box-shadow:
    0 24px 80px rgba(0, 0, 0, 0.7),
    0 0 40px rgba(232, 160, 32, 0.06) !important;
}

/* ── Navigation Drawer Items ── */
.v-list-item {
  border-radius: 8px !important;
  margin: 2px 8px !important;
  transition: all 0.15s ease !important;
}
.v-list-item--active {
  background: rgba(232, 160, 32, 0.1) !important;
  border-left: 2px solid var(--clr-amber) !important;
}
.v-list-item--active .v-list-item-title {
  color: var(--clr-amber) !important;
  font-weight: 500 !important;
}
.v-list-item--active .v-icon {
  color: var(--clr-amber) !important;
}
.v-list-item:hover:not(.v-list-item--active) {
  background: rgba(255, 255, 255, 0.04) !important;
}
.v-list-item-title {
  font-family: var(--font-body) !important;
  font-size: 0.875rem !important;
  font-weight: 400 !important;
  color: var(--clr-text-muted) !important;
}

/* ── Divider ── */
.v-divider {
  border-color: var(--clr-border) !important;
  opacity: 1 !important;
}

/* ── Alert ── */
.v-alert {
  border-radius: 8px !important;
  font-family: var(--font-body) !important;
  font-size: 0.85rem !important;
}

/* ── Stat Card Accent ── */
.stat-card {
  position: relative;
  overflow: hidden;
}
.stat-card::before {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: var(--card-accent, var(--clr-amber));
  opacity: 0.7;
}

/* ── Router link in text ── */
a {
  color: var(--clr-amber);
  text-decoration: none;
}
a:hover {
  color: var(--clr-amber-bright);
  text-decoration: underline;
}
</style>
