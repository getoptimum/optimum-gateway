// https://vitepress.dev/guide/custom-theme
import { h } from "vue";
import type { Theme } from "vitepress";
import DefaultTheme from 'vitepress/theme-without-fonts';
import "./style.css";
import "./fonts.css";
import VersionSwitcher from "./VersionSwitcher.vue";

export default {
  extends: DefaultTheme,
  Layout: () => {
    return h(DefaultTheme.Layout, null, {
      "nav-bar-content-before": () => h(VersionSwitcher),
    });
  },
} satisfies Theme;
