// Public reading has no React theme provider. Generate its highlights from the
// same installed BlockNote constants that feed the native editor, so there is
// one palette and a dependency upgrade cannot silently leave a copied palette.
import {readFileSync, writeFileSync} from 'node:fs';
const source=readFileSync('node_modules/@blocknote/core/src/editor/defaultColors.ts','utf8');
let css='/* Generated from installed @blocknote/core; do not edit. */\n';
for(const [theme,name] of [['light','COLORS_DEFAULT'],['dark','COLORS_DARK_MODE_DEFAULT']]){
 const part=source.split('export const '+name+' = {')[1]?.split('} as Record')[0];
 if(!part)throw new Error('Native BlockNote highlight constants missing');
 const entries=[...part.matchAll(/(gray|brown|red|orange|yellow|green|blue|purple|pink):\s*\{\s*text:\s*"(#[0-9a-f]{6})",\s*background:\s*"(#[0-9a-f]{6})"/gi)];
 if(entries.length!==9 || new Set(entries.map(e=>e[1])).size!==9)throw new Error('Native highlight schema changed: review before release');
 for(const [,color,text,background] of entries){
  css+=`[data-theme="${theme}"] .public-blocks [data-text-color="${color}"]{color:${text}}\n`;
  css+=`[data-theme="${theme}"] .public-blocks [data-background-color="${color}"]{background-color:${background}}\n`;
 }
}
writeFileSync('public/public-block-colors.css',css);
console.log('public native highlights: 9 colors × light/dark, generated from BlockNote');
