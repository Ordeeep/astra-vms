import {createRequire} from "node:module";
import {mkdir,writeFile} from "node:fs/promises";
import {fileURLToPath} from "node:url";
import path from "node:path";

const require=createRequire(import.meta.url);
const root=fileURLToPath(new URL("../../",import.meta.url));
const directory=fileURLToPath(new URL("./",import.meta.url));
const output=path.join(directory,".generated");
await mkdir(output,{recursive:true});
// Use the webpack and SWC already bundled with Next; no fixture dependencies are installed.
const loader=path.join(output,"typescript-loader.cjs");
await require("next/dist/build/swc").loadBindings();
await writeFile(loader,`const swc=require(${JSON.stringify(require.resolve("next/dist/build/swc"))});
module.exports=function(source){return swc.transformSync(source,{filename:this.resourcePath,jsc:{parser:{syntax:"typescript",tsx:this.resourcePath.endsWith(".tsx")},target:"es2022",transform:{react:{runtime:"automatic"}}},module:{type:"es6"}}).code;};`);
const emptyLoader=path.join(output,"empty-loader.cjs");
await writeFile(emptyLoader,'module.exports=function(){return "";};');
const {webpack}=require("next/dist/compiled/webpack/webpack");
await new Promise((resolve,reject)=>{
 const compiler=webpack({
  mode:"production",context:root,entry:path.join(directory,"fixture.tsx"),
  output:{path:output,filename:"fixture.js"},
  resolve:{extensions:[".tsx",".ts",".js",".mjs",".json"],alias:{"@":path.join(root,"src")}},
  module:{rules:[{test:/\.tsx?$/,exclude:/node_modules/,use:loader},{test:/\.css$/,use:emptyLoader}]},
  optimization:{minimize:false},performance:{hints:false},devtool:false,
 });
 compiler.run((error,stats)=>compiler.close(closeError=>{
  if(error||closeError||stats.hasErrors())return reject(error??closeError??new Error(stats.toString({all:false,errors:true,errorDetails:true})));
  console.log(stats.toString({all:false,timings:true,assets:true}));resolve();
 }));
});
const postcss=require("postcss");
const tailwind=require("@tailwindcss/postcss");
const source='@import "../../src/app/globals.css";\n@import "../../src/components/data-table.css";\n\n';
const result=await postcss([tailwind()]).process(source,{from:path.join(directory,"fixture.css"),to:path.join(output,"fixture.css")});
await writeFile(path.join(output,"fixture.css"),result.css);
console.log("Camera form fixture generated. Run: node tests/camera-form/serve.mjs");
