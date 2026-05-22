# OmniRelay Frontend Mobile Optimization Design

> Date: 2026-05-22
> Scope: All authenticated dashboard views + Login/Register
> Approach: CSS + Mobile Card UI for tables (approved)

---

## 1. Layout & Navigation

**Affected File:** `frontend/src/layouts/DefaultLayout.vue`

### Mobile (`<= 768px`)
- **Hide sidebar completely.** `v-navigation-drawer` does not render.
- **Bottom tab bar:** Fixed at bottom, height `56px`, white background with top border (`rgba(0,0,0,0.05)`). Icons + labels for: Dashboard, Providers, Models, API Keys, Usage, Logs.
- **Active route highlight:** Bottom tab icon/text uses `#e8a020` when active.
- **Logout:** Small icon button at the far right of the bottom tab bar.
- **Safe area:** Tab bar respects `env(safe-area-inset-bottom)` for notched devices.

### Tablet (`769px` — `1024px`)
- Sidebar enters Vuetify `rail` mode (icons only, width `56px`).
- Logout icon remains at the bottom rail.

### Desktop (`> 1024px`)
- Unchanged from current behavior.

---

## 2. Data Tables → Mobile Card UI

**Affected Views:**
- `frontend/src/views/ProvidersView.vue`
- `frontend/src/views/ModelsView.vue`
- `frontend/src/views/ApiKeysView.vue`
- `frontend/src/views/UsageView.vue`
- `frontend/src/views/LogsView.vue`

### Strategy
On mobile (`<= 768px`), the existing `<v-table>` is hidden via CSS (`display: none`), and a **card list** is rendered instead.

### Common Component
Create `frontend/src/components/MobileDataCard.vue` to avoid duplication.
- Props: `title`, `items` (array of key/value objects), `actions` (optional slot for Edit/Delete).
- Styling: `border-radius: 12px`, white background, subtle shadow (`0 1px 3px rgba(0,0,0,0.1)`), padding `16px`, margin-bottom `12px`.
- Each key/value pair is displayed as a vertical stack: bold label above value text.
- Action buttons (Edit/Delete) are icon-only `v-btn` placed at the bottom-right of the card.

### Pagination
- `ApiKeysView` and `LogsView` pagination controls: on mobile, replace numeric page buttons with simple **Previous / Next** arrow icon buttons centered below the card list.

### Logs-specific
- Logs JSON payload: instead of `<pre>` (which breaks mobile layout), use a horizontally-scrollable `<div>` with `overflow-x: auto`, `white-space: pre`, and a max-height of `120px`. Font size `12px`.

---

## 3. Forms & Dialogs

**Affected Files:** All Add/Edit dialogs in the affected views.

### Mobile Changes
- `v-dialog` uses `fullscreen` prop on mobile (`<= 768px`). This maximizes screen real estate and prevents the dialog from being clipped.
- Inside dialogs, `v-row` columns must enforce `cols="12"` on mobile: every input becomes a single vertical stack.
- Touch targets: all buttons and inputs must have a minimum tap area of `44px`.
- Input font size: `16px` (prevents iOS auto-zoom on focus).
- Dialog action buttons (Cancel / Save) are pinned at the bottom inside the dialog card, not floating mid-scroll.

---

## 4. Auth Pages

**Affected Files:**
- `frontend/src/views/LoginView.vue`
- `frontend/src/views/RegisterView.vue`

### Mobile Changes
- Centered card fills the viewport: remove side margins (`margin: 0`), remove border-radius (`border-radius: 0`), and let `v-card` stretch edge-to-edge.
- Minimum inner content width: `280px` (prevents extreme squishing).
- Inputs stack vertically with comfortable spacing (`gap: 16px`).
- Links ("Create account", "Forgot password?") have a tap target of at least `44px` height.

---

## 5. Dashboard & Charts

**Affected File:** `frontend/src/views/DashboardView.vue`

### Mobile Changes
- Extend existing `@media (max-width: 768px)` styles. Add an additional `@media (max-width: 480px)` breakpoint for very small screens.
- `v-row` columns become `cols="12"` on mobile: every widget becomes a single column.
- Chart canvas height: reduce to `250px` on mobile so the page remains scrollable without the chart dominating.
- Card gaps: reduce from `24px` to `8px` between cards.

---

## 6. Global Content Padding & Typography

**Affected File:** `frontend/src/styles/page-shared.css`

### Mobile Changes (`@media (max-width: 768px)`)
- `.page-container` padding: `8px` (down from `24px`).
- `.page-title` font size: `20px` (down from `24px`).
- Body text: ensure `text-overflow: ellipsis` or `word-break: break-word` on long strings to prevent horizontal overflow.
- Code blocks (`pre`, `code`): allow `overflow-x: auto` so code snippets scroll horizontally instead of breaking layout.

---

## 7. Breakpoint & CSS Architecture

- **Primary breakpoint:** `768px` (Vuetify `sm` boundary).
- **Secondary breakpoint:** `480px` only for Dashboard chart adjustments.
- **Media query syntax:** `@media (max-width: 768px)`.
- **CSS placement:**
  - Global mobile utilities → `page-shared.css`
  - Component-specific overrides → `<style scoped>` in each view
  - Dialog/Layout logic → inside `DefaultLayout.vue` or scoped dialog styles
- No new external CSS libraries are introduced. Changes use existing Vuetify utilities + custom `<style scoped>` blocks.

---

## 8. Component Inventory

| Component | Status | Path |
|-----------|--------|------|
| `MobileDataCard.vue` | **Create** | `frontend/src/components/MobileDataCard.vue` |
| `DefaultLayout.vue` | **Modify** | `frontend/src/layouts/DefaultLayout.vue` |
| `page-shared.css` | **Modify** | `frontend/src/styles/page-shared.css` |
| `DashboardView.vue` | **Modify** | `frontend/src/views/DashboardView.vue` |
| `ProvidersView.vue` | **Modify** | `frontend/src/views/ProvidersView.vue` |
| `ModelsView.vue` | **Modify** | `frontend/src/views/ModelsView.vue` |
| `ApiKeysView.vue` | **Modify** | `frontend/src/views/ApiKeysView.vue` |
| `UsageView.vue` | **Modify** | `frontend/src/views/UsageView.vue` |
| `LogsView.vue` | **Modify** | `frontend/src/views/LogsView.vue` |
| `LoginView.vue` | **Modify** | `frontend/src/views/LoginView.vue` |
| `RegisterView.vue` | **Modify** | `frontend/src/views/RegisterView.vue` |

---

## 9. Out of Scope

- No changes to Go backend.
- No changes to Vuetify theme or color palette.
- No new dependencies.
- No native mobile app or PWA changes.
- No dark-mode-specific mobile styling (follows existing dark mode rules).
