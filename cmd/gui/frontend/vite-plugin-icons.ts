// Build-time icon pipeline. Reads the SVG originals in assets/svg/ and
// exposes them as the `virtual:icons` module, so dropping a new file in that
// folder is all it takes to change an icon. See assets/svg/README.md for
// the file contract.
//
//   brand/logo-<name>.svg       colour original, shown as drawn (light theme)
//   brand/logo-<name>-dark.svg  single-colour version, recoloured (dark theme)
//   ui/<name>.svg               single-colour top-bar icon, recoloured
//
// Colour originals become data URIs rendered through <img>, which keeps their
// gradient and filter ids isolated. Single-colour files are inlined with
// every paint turned into currentColor so they follow the theme; a gradient,
// filter, mask, pattern or image in one of them fails the build.
import fs from 'node:fs';
import path from 'node:path';
import { optimize, type CustomPlugin, type PluginConfig } from 'svgo';
import type { Plugin } from 'vite';

const VIRTUAL_ID = 'virtual:icons';
const RESOLVED_ID = '\0' + VIRTUAL_ID;
const BRAND_FILE = /^logo-([a-z0-9]+(?:-[a-z0-9]+)*?)(-dark)?\.svg$/;
const UI_FILE = /^([a-z0-9]+(?:-[a-z0-9]+)*)\.svg$/;
const NOT_MONO = /<(linearGradient|radialGradient|filter|mask|pattern|image)\b|url\(#/;

// Drop editor namespaces (Boxy SVG's bx:, Inkscape, Sodipodi…) but keep the
// standard xlink: and xml: prefixes.
const stripEditorData: CustomPlugin = {
  name: 'stripEditorData',
  fn: () => ({
    element: {
      enter: (node, parent) => {
        if (node.name.includes(':')) {
          parent.children = parent.children.filter((c) => c !== node);
          return;
        }
        for (const attr of Object.keys(node.attributes)) {
          const prefix = attr.includes(':') ? attr.split(':')[0] : '';
          const keep = prefix === '' || prefix === 'xlink' || prefix === 'xml'
            || (prefix === 'xmlns' && attr === 'xmlns:xlink');
          if (!keep) delete node.attributes[attr];
        }
      },
    },
  }),
};

// Drop the hidden guide box (and anything else hidden): the files keep it so
// every icon is drawn inside the same frame, but it must not reach the app.
const dropHidden: CustomPlugin = {
  name: 'dropHidden',
  fn: () => ({
    element: {
      enter: (node, parent) => {
        if (node.attributes.visibility === 'hidden' || node.attributes.display === 'none') {
          parent.children = parent.children.filter((c) => c !== node);
        }
      },
    },
  }),
};

// Single-colour shapes without their own fill inherit the root's, which
// defaults to black; make that currentColor too.
const rootFillCurrentColor: CustomPlugin = {
  name: 'rootFillCurrentColor',
  fn: () => ({
    element: {
      enter: (node, parent) => {
        if (node.name === 'svg' && parent.type === 'root' && node.attributes.fill === undefined) {
          node.attributes.fill = 'currentColor';
        }
      },
    },
  }),
};

function clean(file: string, id: string, mono: boolean): string {
  const plugins: PluginConfig[] = [
    'convertStyleToAttrs',
    stripEditorData,
    dropHidden,
    { name: 'preset-default', params: { overrides: { convertColors: mono ? { currentColor: true } : {} } } },
    'removeDimensions',
    { name: 'prefixIds', params: { prefix: id } },
  ];
  if (mono) plugins.push(rootFillCurrentColor);
  const svg = optimize(fs.readFileSync(file, 'utf8'), { path: file, multipass: true, plugins }).data;
  if (mono && NOT_MONO.test(svg)) {
    throw new Error(`${file}: single-colour icons can't use gradients, filters, masks, patterns or images `
      + `(found ${svg.match(NOT_MONO)![0]}). Flatten it to one colour; see assets/svg/README.md.`);
  }
  return svg;
}

function svgFiles(dir: string): string[] {
  return fs.existsSync(dir) ? fs.readdirSync(dir).filter((f) => f.endsWith('.svg')).sort() : [];
}

export default function icons(): Plugin {
  let dir = '';
  return {
    name: 'gitbox-icons',
    configResolved(config) {
      dir = path.resolve(config.root, '../../../assets/svg');
    },
    resolveId(id) {
      return id === VIRTUAL_ID ? RESOLVED_ID : null;
    },
    load(id) {
      if (id !== RESOLVED_ID) return null;
      const brand: Record<string, { color?: string; mono?: string }> = {};
      const ui: Record<string, string> = {};
      for (const f of svgFiles(path.join(dir, 'brand'))) {
        const file = path.join(dir, 'brand', f);
        this.addWatchFile(file);
        const m = BRAND_FILE.exec(f);
        if (!m) { this.warn(`ignoring ${file}: expected logo-<name>.svg or logo-<name>-dark.svg`); continue; }
        const [, name, dark] = m;
        const entry = (brand[name] ||= {});
        if (dark) entry.mono = clean(file, `brand-${name}`, true);
        else entry.color = 'data:image/svg+xml,' + encodeURIComponent(clean(file, `brand-${name}`, false));
      }
      for (const f of svgFiles(path.join(dir, 'ui'))) {
        const file = path.join(dir, 'ui', f);
        this.addWatchFile(file);
        const m = UI_FILE.exec(f);
        if (!m) { this.warn(`ignoring ${file}: expected <name>.svg`); continue; }
        ui[m[1]] = clean(file, `ui-${m[1]}`, true);
      }
      return `export const brand = ${JSON.stringify(brand)};\nexport const ui = ${JSON.stringify(ui)};\n`;
    },
    configureServer(server) {
      // wails dev: a copied-in or edited icon reloads the window.
      server.watcher.add(dir);
      server.watcher.on('all', (_event, file) => {
        if (!file.startsWith(dir + path.sep) || !file.endsWith('.svg')) return;
        const mod = server.moduleGraph.getModuleById(RESOLVED_ID);
        if (mod) server.moduleGraph.invalidateModule(mod);
        server.ws.send({ type: 'full-reload' });
      });
    },
  };
}
