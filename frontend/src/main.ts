import { createPinia } from "pinia";
import { createApp } from "vue";
import App from "./App.vue";
import { i18n } from "./i18n";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/layout.css";
import "./styles/workbench.css";
import "./styles/tailwind.css";

document.documentElement.classList.add("overflow-x-clip");
document.body.classList.add("overflow-x-clip");
createApp(App).use(createPinia()).use(i18n).mount("#app");
