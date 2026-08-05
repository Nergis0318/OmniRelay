<template>
  <div class="auth-shell">
    <!-- Background grid -->
    <div class="auth-grid" aria-hidden="true" />
    <!-- Glow -->
    <div class="auth-glow" aria-hidden="true" />

    <div class="auth-card-wrap">
      <!-- Brand mark -->
      <div class="auth-brand">
        <img class="auth-brand__logo" :src="logoUrl" alt="OmniRelay" />
        <span class="auth-brand__name">OmniRelay</span>
      </div>

      <div class="auth-card">
        <div class="auth-card__header">
          <h1 class="auth-card__title">{{ title }}</h1>
          <p class="auth-card__sub">{{ subtitle }}</p>
        </div>

        <slot />

        <div v-if="$slots.footer" class="auth-footer">
          <slot name="footer" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import logoUrl from "../assets/omnirelay-logo.svg";

defineProps<{
  title: string;
  subtitle: string;
}>();
</script>

<style>
@import "../styles/auth.css";
</style>
