<script setup lang="ts">
import { X } from "lucide-vue-next";
import { ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { tr } from "../i18n";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const dialog = ref<HTMLElement | null>(null);
useModalFocus(dialog, () => appStore.closeAbout());
</script>

<template>
  <div class="dialog-backdrop" @mousedown.self="appStore.closeAbout()">
    <section ref="dialog" class="dialog-window about-dialog" role="dialog" aria-modal="true" aria-labelledby="about-title" tabindex="-1">
      <header>
        <h2 id="about-title">{{ tr("about.title") }}</h2>
        <button class="icon-button" type="button" :title="tr('common.close')" @click="appStore.closeAbout()"><X :size="17" /></button>
      </header>
      <div class="dialog-body about-content">
        <div class="about-product">
          <span class="about-mark" aria-hidden="true">Pi</span>
          <div>
            <strong>Pi Desk</strong>
            <span>{{ tr("about.tagline") }}</span>
          </div>
        </div>
        <p>{{ tr("about.description") }}</p>
        <dl class="about-versions">
          <div><dt>Pi Desk</dt><dd>{{ appStore.bootstrap?.appVersion || "-" }}</dd></div>
          <div><dt>Pi</dt><dd>{{ appStore.bootstrap?.runtime.version || tr("common.unavailable") }}</dd></div>
          <div><dt>Wails</dt><dd>{{ appStore.bootstrap?.wailsVersion || "-" }}</dd></div>
        </dl>
        <p class="about-note">{{ tr("about.note") }}</p>
      </div>
      <footer>
        <button class="text-button primary" type="button" autofocus @click="appStore.closeAbout()">{{ tr("common.close") }}</button>
      </footer>
    </section>
  </div>
</template>
