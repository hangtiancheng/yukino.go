import compiledCss from "../index.css?inline";

const sheet = new CSSStyleSheet();
sheet.replaceSync(compiledCss);

export const twSheet: CSSStyleSheet = sheet;
