import tseslint from "typescript-eslint";
import globals from "globals";

export default tseslint.config(
  ...tseslint.configs.recommended,
  {
    files: ["src/frontend/ts/**/*.ts"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: {
        ...globals.browser,
        marked: "readonly",
      },
    },
    rules: {
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
      "no-console": "off",
    },
  },
);
