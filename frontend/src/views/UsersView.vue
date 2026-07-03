<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ $t("users.title") }}</h1>
        <p class="page-sub">{{ $t("users.subtitle") }}</p>
      </div>
    </div>

    <div class="table-card">
      <v-data-table
        :headers="headers"
        :items="store.users"
        :loading="store.loading"
        density="comfortable"
        hide-default-footer
        :items-per-page="-1"
      >
        <template #item.username="{ item }">
          <span class="username-cell">
            <v-icon size="16" class="user-icon">mdi-account-circle-outline</v-icon>
            {{ item.username }}
          </span>
        </template>
        <template #item.email="{ item }">
          <span class="dim-text">{{ item.email }}</span>
        </template>
        <template #item.is_admin="{ item }">
          <span
            class="status-chip"
            :class="item.is_admin ? 'status-chip--on' : 'status-chip--off'"
          >
            {{ item.is_admin ? $t("users.admin") : $t("users.member") }}
          </span>
        </template>
        <template #item.created_at="{ item }">
          <span class="dim-text">{{
            new Date(item.created_at).toLocaleDateString()
          }}</span>
        </template>
        <template #no-data>
          <div class="empty-state">
            <v-icon size="32" color="#4a4844">mdi-account-group-outline</v-icon>
            <p>{{ $t("users.noUsers") }}</p>
          </div>
        </template>
      </v-data-table>
    </div>

    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="u in store.users"
        :key="u.id"
        :items="[
          { label: $t('users.username'), value: u.username },
          { label: $t('users.email'), value: u.email },
          {
            label: $t('users.role'),
            value: u.is_admin ? $t('users.admin') : $t('users.member'),
          },
          {
            label: $t('users.created'),
            value: new Date(u.created_at).toLocaleDateString(),
          },
        ]"
      />
      <div v-if="!store.users.length" class="empty-state">
        <v-icon size="32" color="#4a4844">mdi-account-group-outline</v-icon>
        <p>{{ $t("users.noUsers") }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useI18n } from "vue-i18n";
import { useUsersStore } from "../stores/users";
import MobileDataCard from "../components/MobileDataCard.vue";

const { t } = useI18n();
const store = useUsersStore();

const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}

const headers = computed(() => [
  { title: t("users.username"), key: "username" },
  { title: t("users.email"), key: "email" },
  { title: t("users.role"), key: "is_admin" },
  { title: t("users.created"), key: "created_at" },
]);

onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  store.fetch();
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});
</script>

<style scoped>
@import "../styles/page-shared.css";

.mobile-cards {
  display: none;
}
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
}

.dim-text {
  font-family: "DM Sans", sans-serif;
  font-size: 0.825rem;
  color: #7c7a75;
}

.username-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  font-family: "DM Sans", sans-serif;
  font-size: 0.875rem;
  font-weight: 500;
  color: #e8e6e1;
}
.user-icon {
  color: #7c7a75;
  flex-shrink: 0;
}
</style>
