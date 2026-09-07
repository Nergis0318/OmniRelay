<template>
  <div class="page">
    <PageHeader :title="$t('providers.title')" :subtitle="$t('providers.subtitle')">
      <button v-if="isAdmin" class="btn-primary" @click="openDialog()">
        <v-icon size="15">mdi-plus</v-icon>
        {{ $t("providers.addProvider") }}
      </button>
    </PageHeader>

    <AppAlert v-if="testResult && testResult.ok" variant="success" page>
      {{ $t("providers.testSuccess", { latency: testResult.latency_ms }) }}
    </AppAlert>
    <AppAlert v-if="testResult && !testResult.ok" variant="error" page>
      {{ testResult.error || $t("providers.testFailed") }}
    </AppAlert>
    <AppAlert v-if="syncResult" variant="success" page>{{ syncResult }}</AppAlert>
    <AppAlert v-if="syncError" variant="error" page>{{ syncError }}</AppAlert>

    <div class="table-card">
      <v-data-table
        :headers="headers"
        :items="store.providers"
        :loading="store.loading"
        density="comfortable"
        hide-default-footer
        :items-per-page="-1"
      >
        <template #item.provider_key="{ item }">
          <MonoTag>{{ item.provider_key }}</MonoTag>
        </template>
        <template #item.is_active="{ item }">
          <StatusChip :variant="item.is_active ? 'on' : 'off'">
            {{ item.is_active ? $t("providers.active") : $t("providers.inactive") }}
          </StatusChip>
        </template>
        <template #item.provider_type="{ item }">
          <span class="type-chip">{{ item.provider_type }}</span>
        </template>
        <template #item.actions="{ item }">
          <div class="row-actions">
            <button
              class="row-btn"
              :title="$t('providers.test')"
              :disabled="testingId === item.id"
              @click="handleTest(item.id)"
            >
              <v-icon v-if="testingId !== item.id" size="15">mdi-connection</v-icon>
              <span v-else class="btn-spinner btn-spinner--sm" />
            </button>
            <button class="row-btn" title="Edit" @click="openDialog(item)">
              <v-icon size="15">mdi-pencil-outline</v-icon>
            </button>
            <button
              class="row-btn"
              title="Sync Models"
              :disabled="syncingId === item.id"
              @click="handleSync(item.id)"
            >
              <v-icon v-if="syncingId !== item.id" size="15">mdi-sync</v-icon>
              <span v-else class="btn-spinner btn-spinner--sm" />
            </button>
            <button
              class="row-btn row-btn--danger"
              title="Delete"
              @click="handleDelete(item.id)"
            >
              <v-icon size="15">mdi-delete-outline</v-icon>
            </button>
          </div>
        </template>
        <template #no-data>
          <EmptyState icon="mdi-server-off" :text="$t('providers.noProviders')" />
        </template>
      </v-data-table>
    </div>

    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="p in store.providers"
        :key="p.id"
        :items="[
          { label: $t('providers.key'), value: p.provider_key },
          { label: $t('providers.name'), value: p.name },
          { label: $t('providers.type'), value: p.provider_type },
          {
            label: $t('providers.status'),
            value: p.is_active
              ? $t('providers.active')
              : $t('providers.inactive'),
          },
        ]"
      >
        <template v-if="isAdmin" #actions>
          <button
            class="row-btn"
            :title="$t('providers.test')"
            :disabled="testingId === p.id"
            @click="handleTest(p.id)"
          >
            <v-icon v-if="testingId !== p.id" size="15">mdi-connection</v-icon>
            <span v-else class="btn-spinner btn-spinner--sm" />
          </button>
          <button class="row-btn" title="Edit" @click="openDialog(p)">
            <v-icon size="15">mdi-pencil-outline</v-icon>
          </button>
          <button
            class="row-btn"
            title="Sync Models"
            :disabled="syncingId === p.id"
            @click="handleSync(p.id)"
          >
            <v-icon v-if="syncingId !== p.id" size="15">mdi-sync</v-icon>
            <span v-else class="btn-spinner btn-spinner--sm" />
          </button>
          <button
            class="row-btn row-btn--danger"
            title="Delete"
            @click="handleDelete(p.id)"
          >
            <v-icon size="15">mdi-delete-outline</v-icon>
          </button>
        </template>
      </MobileDataCard>
      <EmptyState
        v-if="!store.providers.length"
        icon="mdi-server-off"
        :text="$t('providers.noProviders')"
      />
    </div>

    <!-- Dialog -->
    <v-dialog
      v-model="dialog"
      :max-width="isMobile ? undefined : 520"
      :fullscreen="isMobile"
    >
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title">
            {{
              editing
                ? $t("providers.editProvider")
                : $t("providers.addProvider")
            }}
          </h2>
          <button class="dialog-close" @click="dialog = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>

        <div class="dialog-body">
          <div class="field-group">
            <label class="field-label">{{ $t("providers.providerKey") }}</label>
            <input
              v-model="form.provider_key"
              class="field-input"
              placeholder="e.g. openai, my-llama"
              :disabled="!!editing"
            />
          </div>
          <div class="field-group">
            <label class="field-label">{{ $t("providers.displayName") }}</label>
            <input
              v-model="form.name"
              class="field-input"
              placeholder="e.g. OpenAI"
            />
          </div>
          <div v-if="form.provider_type !== 'custom'" class="field-group">
            <label class="field-label">{{ $t("providers.apiBaseUrl") }}</label>
            <input
              v-model="form.api_base_url"
              class="field-input"
              placeholder="https://api.openai.com/v1"
            />
          </div>
          <div v-if="form.provider_type !== 'custom' && !editing" class="field-group">
            <label class="field-label">{{ $t("providers.apiKey") }}</label>
            <input
              v-model="form.api_key"
              type="password"
              class="field-input"
              placeholder="sk-..."
            />
          </div>
          <div v-if="form.provider_type !== 'custom' && editing" class="field-group">
            <label class="field-label">{{ $t("providers.apiKeys") }}</label>
            <div
              v-for="key in editing.api_keys"
              :key="key.id"
              class="key-row"
            >
              <span class="key-prefix" :title="$t('providers.keyPrefix')">{{
                key.key_prefix
              }}</span>
              <label class="checkbox-row key-active">
                <input
                  type="checkbox"
                  class="checkbox"
                  :checked="key.is_active"
                  @click.prevent="handleSetKeyActive(key.id, !key.is_active)"
                />
                <span class="checkbox-label">{{ $t("providers.active") }}</span>
              </label>
              <button
                type="button"
                class="row-btn row-btn--danger"
                :title="$t('common.delete')"
                @click="handleRemoveKey(key.id)"
              >
                <v-icon size="15">mdi-delete-outline</v-icon>
              </button>
            </div>
            <div class="endpoint-row">
              <input
                v-model="newKey"
                type="password"
                class="field-input"
                placeholder="sk-..."
              />
              <button
                type="button"
                class="btn-secondary"
                :disabled="!newKey"
                @click="handleAddKey"
              >
                {{ $t("providers.addKey") }}
              </button>
            </div>
          </div>
          <div class="field-group">
            <label class="field-label">{{
              $t("providers.providerType")
            }}</label>
            <select v-model="form.provider_type" class="field-select">
              <option v-for="t in providerTypes" :key="t" :value="t">
                {{ t }}
              </option>
            </select>
          </div>
          <div v-if="form.provider_type !== 'custom'" class="field-group">
            <label class="field-label">{{
              $t("providers.additionalFormats")
            }}</label>
            <p class="field-hint">{{ $t("providers.additionalFormatsHint") }}</p>
            <div
              v-for="(ep, i) in form.endpoints"
              :key="i"
              class="endpoint-row"
            >
              <select v-model="ep.api_type" class="field-select endpoint-select">
                <option v-for="t in endpointTypes" :key="t" :value="t">
                  {{ t }}
                </option>
              </select>
              <input
                v-model="ep.base_url"
                class="field-input"
                placeholder="https://..."
              />
              <button
                class="row-btn row-btn--danger"
                :title="$t('common.delete')"
                @click="form.endpoints.splice(i, 1)"
              >
                <v-icon size="15">mdi-close</v-icon>
              </button>
            </div>
            <button
              type="button"
              class="btn-secondary"
              @click="form.endpoints.push({ api_type: 'openai', base_url: '' })"
            >
              <v-icon size="14">mdi-plus</v-icon>
              {{ $t("providers.addFormat") }}
            </button>
          </div>
          <div v-if="form.provider_type === 'custom'" class="field-group">
            <label class="field-label">{{
              $t("providers.sourceModels")
            }}</label>
            <p class="field-hint">{{ $t("providers.selectModelsHint") }}</p>
            <div class="source-models-list">
              <div
                v-for="group in store.sourceModels"
                :key="group.provider_key"
                class="source-model-group"
              >
                <div class="source-model-group-header">
                  <span class="source-model-group-name">{{ group.name }}</span>
                  <span class="source-model-group-type">{{
                    group.provider_type
                  }}</span>
                  <span class="source-model-group-key">{{
                    group.provider_key
                  }}</span>
                </div>
                <div
                  v-for="model in group.models"
                  :key="model.model_id"
                  class="source-model-item"
                >
                  <label class="source-model-label">
                    <input
                      type="checkbox"
                      :value="`${group.provider_key}/${model.model_id}`"
                      v-model="form.source_models"
                      class="checkbox"
                    />
                    <span class="source-model-id">{{ model.model_id }}</span>
                  </label>
                </div>
              </div>
              <div
                v-if="!store.sourceModels.length"
                class="empty-source-models"
              >
                {{ $t("providers.noSourceModels") }}
              </div>
            </div>
          </div>
          <label class="checkbox-row">
            <input
              type="checkbox"
              v-model="form.show_in_model_list"
              class="checkbox"
            />
            <div>
              <span class="checkbox-label">{{
                $t("providers.showInModelList")
              }}</span>
              <span class="checkbox-hint">{{
                $t("providers.showInModelListHint")
              }}</span>
            </div>
          </label>
          <label v-if="form.provider_type !== 'custom'" class="checkbox-row">
            <input type="checkbox" v-model="form.auto_sync" class="checkbox" />
            <div>
              <span class="checkbox-label">{{ $t("providers.autoSync") }}</span>
              <span class="checkbox-hint">{{
                $t("providers.autoSyncHint")
              }}</span>
            </div>
          </label>

          <AppAlert v-if="syncResult" variant="success">{{ syncResult }}</AppAlert>
          <AppAlert v-if="dialogError || store.error" variant="error">{{
            dialogError || store.error
          }}</AppAlert>
        </div>

        <div class="dialog-footer">
          <button class="btn-ghost" @click="dialog = false">
            {{ $t("common.cancel") }}
          </button>
          <button class="btn-primary" @click="handleSave" :disabled="saving">
            <span v-if="!saving">{{
              editing ? $t("common.update") : $t("common.create")
            }}</span>
            <span v-else class="btn-spinner" />
          </button>
        </div>
      </div>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { useProvidersStore } from "../stores/providers";
import { useAuthStore } from "../stores/auth";
import MobileDataCard from "../components/MobileDataCard.vue";
import PageHeader from "../components/PageHeader.vue";
import EmptyState from "../components/EmptyState.vue";
import StatusChip from "../components/StatusChip.vue";
import MonoTag from "../components/MonoTag.vue";
import AppAlert from "../components/AppAlert.vue";
import { useMobile } from "../composables/useMobile";

const { t } = useI18n();
const store = useProvidersStore();
const auth = useAuthStore();
const isAdmin = computed(() => !!auth.user?.is_admin);
const { isMobile } = useMobile();

onMounted(() => {
  store.fetch();
  store.fetchSourceModels();
});

const dialog = ref(false);
const editing = ref<any>(null);
const saving = ref(false);
const dialogError = ref("");
const newKey = ref("");
const syncResult = ref("");
const syncError = ref("");
const syncingId = ref<number | null>(null);
const testingId = ref<number | null>(null);
const testResult = ref<{ ok: boolean; latency_ms: number; error?: string } | null>(null);
const providerTypes = [
  "custom",
  "openai",
  "anthropic",
  "lmstudio",
  "ollama",
  "gemini",
];
const endpointTypes = ["openai", "anthropic", "lmstudio", "ollama", "gemini"];

const form = ref({
  provider_key: "",
  name: "",
  api_base_url: "",
  api_key: "",
  provider_type: "openai",
  auto_sync: true,
  show_in_model_list: true,
  source_models: [] as string[],
  endpoints: [] as { api_type: string; base_url: string }[],
});

const headers = computed(() => {
  const cols: Record<string, unknown>[] = [
    { title: t("providers.key"), key: "provider_key", sortable: true },
    { title: t("providers.name"), key: "name", sortable: true },
    { title: t("providers.type"), key: "provider_type" },
    { title: t("providers.status"), key: "is_active" },
  ];
  if (isAdmin.value) {
    cols.push({
      title: t("providers.actions"),
      key: "actions",
      sortable: false,
      align: "end",
    });
  }
  return cols;
});

function openDialog(provider?: any) {
  dialogError.value = "";
  syncResult.value = "";
  syncError.value = "";
  newKey.value = "";
  store.clearError();
  if (provider) {
    editing.value = provider;
    form.value = {
      ...provider,
      api_key: "",
      auto_sync: false,
      source_models: provider.source_models ?? [],
      endpoints: provider.endpoints ? provider.endpoints.map((e: any) => ({ ...e })) : [],
      show_in_model_list: provider.show_in_model_list ?? true,
    };
  } else {
    editing.value = null;
    form.value = {
      provider_key: "",
      name: "",
      api_base_url: "",
      api_key: "",
      provider_type: "openai",
      auto_sync: true,
      show_in_model_list: true,
      source_models: [],
      endpoints: [],
    };
  }
  dialog.value = true;
}

async function handleSave() {
  saving.value = true;
  dialogError.value = "";
  syncResult.value = "";
  try {
    if (editing.value) {
      // For custom providers, include source_models so model selections persist.
      // For non-custom providers, omit it — models are managed via sync.
      const rest = form.value.provider_type === "custom"
        ? (({ auto_sync: _s, api_key: _k, ...r }) => r)(form.value)
        : (({ source_models: _o, auto_sync: _s, api_key: _k, ...r }) => r)(form.value);
      await store.update(editing.value.id, rest);
      if (form.value.auto_sync && form.value.provider_type !== "custom") {
        const { data } = await store.syncModels(editing.value.id);
        syncResult.value = t("providers.syncedModels", {
          count: data.model_count,
        });
      }
    } else {
      const created = await store.create(form.value);
      if (form.value.auto_sync && form.value.provider_type !== "custom") {
        const { data } = await store.syncModels(created.id);
        syncResult.value = t("providers.syncedModels", {
          count: data.model_count,
        });
      }
    }
    dialog.value = false;
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || t("providers.saveFailed");
  } finally {
    saving.value = false;
  }
}

  async function handleSync(id: number) {
    syncResult.value = "";
    syncError.value = "";
    testResult.value = null;
    syncingId.value = id;
    try {
      const { data } = await store.syncModels(id);
      syncResult.value = t("providers.syncedModels", { count: data.model_count });
    } catch (e: any) {
      syncError.value = e.response?.data?.error || t("providers.syncFailed");
    } finally {
      syncingId.value = null;
    }
  }

  async function handleTest(id: number) {
    testResult.value = null;
    syncResult.value = "";
    syncError.value = "";
    testingId.value = id;
    try {
      const result = await store.testProvider(id);
      testResult.value = result;
    } catch (e: any) {
      dialogError.value = e.response?.data?.error || t("providers.testFailed");
    } finally {
      testingId.value = null;
    }
  }

function refreshEditing() {
  if (!editing.value) return;
  const p = store.providers.find((x) => x.id === editing.value.id);
  if (p) editing.value = p;
}

async function handleAddKey() {
  if (!editing.value || !newKey.value) return;
  dialogError.value = "";
  try {
    await store.addKey(editing.value.id, newKey.value);
    newKey.value = "";
    refreshEditing();
  } catch {
    dialogError.value = store.error || t("providers.saveFailed");
  }
}

async function handleSetKeyActive(keyId: number, is_active: boolean) {
  if (!editing.value) return;
  dialogError.value = "";
  try {
    await store.setKeyActive(editing.value.id, keyId, is_active);
    refreshEditing();
  } catch {
    dialogError.value = store.error || t("providers.cannotDeleteLastKey");
  }
}

async function handleRemoveKey(keyId: number) {
  if (!editing.value) return;
  dialogError.value = "";
  try {
    await store.removeKey(editing.value.id, keyId);
    refreshEditing();
  } catch {
    dialogError.value = store.error || t("providers.cannotDeleteLastKey");
  }
}

async function handleDelete(id: number) {
  if (!confirm(t("providers.deleteConfirm"))) return;
  await store.remove(id);
}
</script>

<style scoped>
@import "../styles/page-shared.css";
.mobile-cards {
  display: none;
}
.source-models-list {
  max-height: 300px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 8px;
  margin-top: 4px;
}
.source-model-group {
  margin-bottom: 8px;
}
.source-model-group-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
}
.source-model-group-name {
  font-weight: 600;
  font-size: 13px;
}
.source-model-group-type {
  font-size: 11px;
  background: var(--chip-bg);
  padding: 1px 6px;
  border-radius: 4px;
}
.source-model-group-key {
  font-size: 11px;
  color: var(--text-muted);
}
.source-model-item {
  padding: 2px 0 2px 12px;
}
.source-model-label {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  font-size: 13px;
}
.source-model-id {
  font-size: 12px;
  font-family: monospace;
}
.empty-source-models {
  padding: 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}
.field-hint {
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 4px;
}
.endpoint-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}
.key-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.key-prefix {
  font-family: monospace;
  font-size: 13px;
  flex: 1;
  min-width: 0;
}
.key-active {
  margin: 0;
  flex: 0 0 auto;
}
.endpoint-row .endpoint-select {
  flex: 0 0 120px;
  min-width: 0;
}
.endpoint-row .field-input {
  flex: 1 1 auto;
}
.btn-secondary {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--chip-bg, #eee);
  border: 1px solid var(--border, #ccc);
  border-radius: 6px;
  padding: 4px 10px;
  font-size: 13px;
  cursor: pointer;
}
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
}
</style>
