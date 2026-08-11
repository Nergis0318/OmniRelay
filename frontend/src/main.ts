import { createApp } from "vue";
import { createPinia } from "pinia";
import router from "./plugins/router";
import vuetify from "./plugins/vuetify";
import i18n from "./plugins/i18n";
import App from "./App.vue";
import "@mdi/font/css/materialdesignicons.css";
import "./styles/tokens.css";

const app = createApp(App);
app.use(createPinia());
app.use(router);
app.use(vuetify);
app.use(i18n);
app.mount("#app");
