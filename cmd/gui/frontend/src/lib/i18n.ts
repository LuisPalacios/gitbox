import { writable, derived } from 'svelte/store';

export type Language = 'en' | 'es';

const fallback: Language = 'en';

const catalogs: Record<Language, Record<string, string>> = {
  en: {
    'settings.config': 'Config',
    'settings.openInEditor': 'Open in Editor',
    'settings.rootFolder': 'Root folder',
    'settings.change': 'Change',
    'settings.language': 'Language',
    'settings.theme': 'Theme',
    'settings.periodicStatus': 'Periodic status check',
    'settings.runAtStartup': 'Run at startup',
    'settings.prReviews': 'PR / reviews',
    'settings.includeDrafts': 'Include drafts',
    'settings.globalGitignore': 'Global gitignore',
    'settings.systemCheck': 'System check',
    'settings.run': 'Run',
    'settings.version': 'Version',
    'settings.author': 'Author',
    'common.off': 'Off',
    'common.on': 'On',
  },
  es: {
    'settings.config': 'Configuracion',
    'settings.openInEditor': 'Abrir en editor',
    'settings.rootFolder': 'Carpeta raiz',
    'settings.change': 'Cambiar',
    'settings.language': 'Idioma',
    'settings.theme': 'Tema',
    'settings.periodicStatus': 'Comprobacion periodica',
    'settings.runAtStartup': 'Ejecutar al inicio',
    'settings.prReviews': 'PR / revisiones',
    'settings.includeDrafts': 'Incluir borradores',
    'settings.globalGitignore': 'Gitignore global',
    'settings.systemCheck': 'Comprobacion del sistema',
    'settings.run': 'Ejecutar',
    'settings.version': 'Version',
    'settings.author': 'Autor',
    'common.off': 'No',
    'common.on': 'Si',
  },
};

export const languageStore = writable<Language>(fallback);

export function normalizeLanguage(lang: string | undefined | null): Language {
  return lang === 'es' ? 'es' : 'en';
}

export const t = derived(languageStore, ($lang) => {
  return (key: string): string => catalogs[$lang]?.[key] ?? catalogs[fallback][key] ?? key;
});
