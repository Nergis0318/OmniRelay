import { createVuetify } from "vuetify";
import * as components from "vuetify/components";
import * as directives from "vuetify/directives";
import "vuetify/styles";

const vuetify = createVuetify({
  components,
  directives,
  theme: {
    defaultTheme: "signal",
    themes: {
      signal: {
        dark: true,
        colors: {
          primary: "#E8A020",
          secondary: "#2EC4B6",
          accent: "#F5C842",
          background: "#0D0D0F",
          surface: "#131316",
          "surface-variant": "#1A1A1F",
          error: "#FF5757",
          info: "#2EC4B6",
          success: "#2EC4B6",
          warning: "#E8A020",
          "on-background": "#E8E6E1",
          "on-surface": "#E8E6E1",
          "on-primary": "#0D0D0F",
          "on-secondary": "#0D0D0F",
        },
      },
    },
  },
  defaults: {
    VCard: {
      elevation: 0,
      rounded: "lg",
    },
    VBtn: {
      rounded: "lg",
      style:
        "font-family: 'DM Sans', sans-serif; font-weight: 500; letter-spacing: 0.02em;",
    },
    VTextField: {
      variant: "outlined",
      density: "comfortable",
      color: "primary",
    },
    VSelect: {
      variant: "outlined",
      density: "comfortable",
      color: "primary",
    },
    VChip: {
      rounded: "sm",
    },
    VDataTable: {
      hover: true,
    },
  },
});

export default vuetify;
