<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ $t("providers.title") }}</h1>
        <p class="page-sub">{{ $t("providers.subtitle") }}</p>
      </div>
      <button class="btn-primary" @click="openDialog()">
        <v-icon size="15">mdi-plus</v-icon>
        {{ $t("providers.addProvider") }}
      </button>
    </div>

    <div v-if="testResult && testResult.ok" class="alert alert--success alert--page">
      <v-icon size="14">mdi-check-circle-outline</v-icon>
      {{ $t("providers.testSuccess", { latency: testResult.latency_ms }) }}
    </div>
    <div v-if="testResult && !testResult.ok" class="alert alert--error alert--page">
      <v-icon size="14">mdi-alert-circle-outline</v-icon>
      {{ testResult.error || $t("providers.testFailed") }}
    </div>

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
          <code class="mono-tag">{{ item.provider_key }}</code>
        </template>
        <template #item.is_active="{ item }">
          <span
            class="status-chip"
            :class="item.is_active ? 'status-chip--on' : 'status-chip--off'"
          >
            {{
              item.is_active ? $t("providers.active") : $t("providers.inactive")
            }}
          </span>
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
              @click="handleSync(item.id)"
            >
              <v-icon size="15">mdi-sync</v-icon>
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
          <div class="empty-state">
            <v-icon size="32" color="#4a4844">mdi-server-off</v-icon>
            <p>{{ $t("providers.noProviders") }}</p>
          </div>
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
        <template #actions>
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
          <button class="row-btn" title="Sync Models" @click="handleSync(p.id)">
            <v-icon size="15">mdi-sync</v-icon>
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
      <div v-if="!store.providers.length" class="empty-state">
        <v-icon size="32" color="#4a4844">mdi-server-off</v-icon>
        <p>{{ $t("providers.noProviders") }}</p>
      </div>
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
          <div v-if="form.provider_type !== 'custom'" class="field-group">
            <label class="field-label">{{ $t("providers.apiKey") }}</label>
            <input
              v-model="form.api_key"
              type="password"
              class="field-input"
              :placeholder="editing ? $t('providers.leaveEmpty') : 'sk-...'"
            />
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

          <div v-if="syncResult" class="alert alert--success">
            <v-icon size="14">mdi-check-circle-outline</v-icon>
            {{ syncResult }}
          </div>
          <div v-if="dialogError" class="alert alert--error">
            <v-icon size="14">mdi-alert-circle-outline</v-icon>
            {{ dialogError }}
          </div>
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
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useI18n } from "vue-i18n";
import { useProvidersStore } from "../stores/providers";
import MobileDataCard from "../components/MobileDataCard.vue";

const { t } = useI18n();
const store = useProvidersStore();
const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  store.fetch();
  store.fetchSourceModels();
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});

const dialog = ref(false);
const editing = ref<any>(null);
const saving = ref(false);
const dialogError = ref("");
const syncResult = ref("");
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

const form = ref({
  provider_key: "",
  name: "",
  api_base_url: "",
  api_key: "",
  provider_type: "openai",
  auto_sync: true,
  show_in_model_list: true,
  source_models: [] as string[],
});

const headers = computed(() => [
  { title: t("providers.key"), key: "provider_key", sortable: true },
  { title: t("providers.name"), key: "name", sortable: true },
  { title: t("providers.type"), key: "provider_type" },
  { title: t("providers.status"), key: "is_active" },
  {
    title: t("providers.actions"),
    key: "actions",
    sortable: false,
    align: "end" as const,
  },
]);

function openDialog(provider?: any) {
  dialogError.value = "";
  syncResult.value = "";
  if (provider) {
    editing.value = provider;
    form.value = {
      ...provider,
      api_key: "",
      auto_sync: false,
      source_models: provider.source_models ?? [],
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
        ? (({ auto_sync: _s, ...r }) => r)(form.value)
        : (({ source_models: _o, auto_sync: _s, ...r }) => r)(form.value);
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
    testResult.value = null;
    try {
      const { data } = await store.syncModels(id);
      syncResult.value = t("providers.syncedModels", { count: data.model_count });
    } catch (e: any) {
      dialogError.value = e.response?.data?.error || t("providers.syncFailed");
    }
  }

  async function handleTest(id: number) {
    testResult.value = null;
    syncResult.value = "";
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
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
}
</style>
