/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/*
 * 液态玻璃的 WebGL 渲染器。
 *
 * CSS 路径的性能天花板在于 backdrop-filter 引用 SVG 滤镜会让整层掉出 GPU
 * 合成路径，且每帧都要重新模糊 backdrop。这里反过来：背景是我们自己的静态
 * 图，模糊在图片加载时烘焙一次（逐级降采样到 1/8 + 三轮分离高斯），之后每
 * 帧只是采样；所有玻璃矩形合并成一个批次，加上背景一共 2 个 draw call；
 * 静止时一帧都不画。
 *
 * 光学配方对齐开源实现的共识（shuding/liquid-glass、
 * iyinchao/liquid-glass-studio、ybouane/liquidglass、naughtyduk/liquidGL）：
 *  - 位移沿内法线（向心采样，透镜感）
 *  - 强度曲线 pow(1-t,1.6) 主体 + pow(1-t,10) 贝塞尔尖峰，中心严格为 0
 *  - 色散直接拿折射偏移按比例反向错开 R/B，与位移同曲线
 *  - 边缘光：SDF 梯度补固定 z 抬成 3D 法线做 Blinn-Phong，叠顶亮底暗内描边
 *  - 折射带内把背景还原清晰——重磨砂会毁掉折射细节
 *
 * DOM 卡片保留自己的渐变底色、阴影与高光，只有 backdrop-filter 被画布替代，
 * 所以视觉分层与 CSS 路径一致。
 */

// 与 index.css 里挂 backdrop-filter 的选择器保持同一份清单
export const GLASS_ELEMENT_SELECTORS = [
  '.site-background-glass .semi-card',
  '.site-background-glass .pricing-sidebar',
  '.site-background-glass .pricing-search-header',
  '.site-background-glass .semi-layout-header > header',
  '.site-background-glass .app-sider',
  '.site-background-glass .semi-layout-footer',
].join(', ');

const MAX_PIXEL_RATIO = 1.5;
const MAX_TRACKED_ELEMENTS = 96;

const VS_QUAD = [
  'attribute vec2 a_pos;',
  'varying vec2 v_uv;',
  'void main() {',
  '  gl_Position = vec4(a_pos, 0.0, 1.0);',
  '  v_uv = a_pos * 0.5 + 0.5;',
  '}',
].join('\n');

// 背景：按 fit 变换采样，取样落到图片之外时给页面底色（contain 的留边）
const FS_BG = [
  'precision mediump float;',
  'varying vec2 v_uv;',
  'uniform sampler2D u_tex;',
  'uniform vec4 u_fit;',
  'uniform vec3 u_bgColor;',
  'uniform vec4 u_overlay;',
  'void main() {',
  '  vec2 uv = v_uv * u_fit.xy + u_fit.zw;',
  '  vec3 c = u_bgColor;',
  '  if (uv.x >= 0.0 && uv.x <= 1.0 && uv.y >= 0.0 && uv.y <= 1.0) {',
  '    c = texture2D(u_tex, uv).rgb;',
  '  }',
  '  c = mix(c, u_overlay.rgb, u_overlay.a);',
  '  gl_FragColor = vec4(c, 1.0);',
  '}',
].join('\n');

// 逐级降采样：4 个双线性 tap 取 2x2 均值，避免直接缩小产生锯齿
const FS_DOWN = [
  'precision mediump float;',
  'varying vec2 v_uv;',
  'uniform sampler2D u_tex;',
  'uniform vec2 u_texel;',
  'void main() {',
  '  vec3 c = texture2D(u_tex, v_uv + u_texel * vec2(0.5, 0.5)).rgb;',
  '  c += texture2D(u_tex, v_uv + u_texel * vec2(-0.5, 0.5)).rgb;',
  '  c += texture2D(u_tex, v_uv + u_texel * vec2(0.5, -0.5)).rgb;',
  '  c += texture2D(u_tex, v_uv + u_texel * vec2(-0.5, -0.5)).rgb;',
  '  gl_FragColor = vec4(c * 0.25, 1.0);',
  '}',
].join('\n');

// 线性采样 9-tap 分离高斯；tap 位置固定，加宽靠多轮叠加而不是拉伸间距
const FS_BLUR = [
  'precision mediump float;',
  'varying vec2 v_uv;',
  'uniform sampler2D u_tex;',
  'uniform vec2 u_step;',
  'void main() {',
  '  vec3 c = texture2D(u_tex, v_uv).rgb * 0.2270270270;',
  '  c += texture2D(u_tex, v_uv + u_step * 1.3846153846).rgb * 0.3162162162;',
  '  c += texture2D(u_tex, v_uv - u_step * 1.3846153846).rgb * 0.3162162162;',
  '  c += texture2D(u_tex, v_uv + u_step * 3.2307692308).rgb * 0.0702702703;',
  '  c += texture2D(u_tex, v_uv - u_step * 3.2307692308).rgb * 0.0702702703;',
  '  gl_FragColor = vec4(c, 1.0);',
  '}',
].join('\n');

const VS_GLASS = [
  'attribute vec2 a_pos;',
  'attribute vec2 a_local;',
  'attribute vec3 a_shape;',
  'varying vec2 v_local;',
  'varying vec3 v_shape;',
  'varying vec2 v_screen;',
  'void main() {',
  '  gl_Position = vec4(a_pos, 0.0, 1.0);',
  '  v_local = a_local;',
  '  v_shape = a_shape;',
  '  v_screen = a_pos * 0.5 + 0.5;',
  '}',
].join('\n');

const FS_GLASS = [
  'precision mediump float;',
  'varying vec2 v_local;',
  'varying vec3 v_shape;',
  'varying vec2 v_screen;',
  'uniform sampler2D u_blur;',
  'uniform sampler2D u_bg;',
  'uniform vec4 u_fit;',
  'uniform vec3 u_bgColor;',
  'uniform vec2 u_res;',
  'uniform float u_bandFrac;',
  'uniform float u_scaleFrac;',
  'uniform float u_dispersion;',
  'uniform float u_clarity;',
  'uniform float u_edgeLight;',
  'uniform vec4 u_overlay;',
  'uniform vec3 u_css;',
  '',
  'float sdRoundBox(vec2 p, vec2 b, float r) {',
  '  vec2 q = abs(p) - b + r;',
  '  return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r;',
  '}',
  '',
  'vec3 sharpAt(vec2 uv) {',
  '  vec2 iuv = uv * u_fit.xy + u_fit.zw;',
  '  if (iuv.x < 0.0 || iuv.x > 1.0 || iuv.y < 0.0 || iuv.y > 1.0) {',
  '    return u_bgColor;',
  '  }',
  '  return texture2D(u_bg, iuv).rgb;',
  '}',
  '',
  'vec3 grab(vec2 uv, float sharpW) {',
  '  vec3 blurred = texture2D(u_blur, uv).rgb;',
  '  return mix(blurred, sharpAt(uv), sharpW);',
  '}',
  '',
  'vec3 applyCss(vec3 c) {',
  '  float l = dot(c, vec3(0.213, 0.715, 0.072));',
  '  c = mix(vec3(l), c, u_css.x);',
  '  c *= u_css.y;',
  '  c = (c - 0.5) * u_css.z + 0.5;',
  '  return clamp(c, 0.0, 1.0);',
  '}',
  '',
  'void main() {',
  '  vec2 halfSize = v_shape.xy;',
  '  float sdf = sdRoundBox(v_local, halfSize, v_shape.z);',
  '  float bandPx =',
  '    max(u_bandFrac * min(halfSize.x, halfSize.y) * 2.0, 1.0);',
  '  float t = clamp(-sdf / bandPx, 0.0, 1.0);',
  '  float curve =',
  '    (pow(1.0 - t, 1.6) + 0.5 * pow(1.0 - t, 10.0)) / 1.5;',
  '',
  '  vec2 e = vec2(1.5, 0.0);',
  '  vec2 grad = vec2(',
  '    sdRoundBox(v_local + e.xy, halfSize, v_shape.z) -',
  '      sdRoundBox(v_local - e.xy, halfSize, v_shape.z),',
  '    sdRoundBox(v_local + e.yx, halfSize, v_shape.z) -',
  '      sdRoundBox(v_local - e.yx, halfSize, v_shape.z));',
  '  vec2 n2 =',
  '    length(grad) > 0.0001 ? normalize(grad) : vec2(0.0);',
  '',
  '  float maxDispPx =',
  '    u_scaleFrac * min(halfSize.x, halfSize.y) * 1.2;',
  '  vec2 offPx = -n2 * (maxDispPx * curve);',
  '  vec2 uvOff = offPx / u_res * vec2(1.0, -1.0);',
  '  vec2 caOff = uvOff * u_dispersion;',
  '  float sharpW = clamp(curve * u_clarity * 1.4, 0.0, 1.0);',
  '  vec3 c;',
  '  c.r = grab(v_screen + uvOff + caOff, sharpW).r;',
  '  c.g = grab(v_screen + uvOff, sharpW).g;',
  '  c.b = grab(v_screen + uvOff - caOff, sharpW).b;',
  '',
  // v_local 的 y 向下为正，左上光源的 y 分量取负
  '  vec3 n3 = normalize(vec3(n2 * curve, 0.6));',
  '  vec3 lightDir = normalize(vec3(-0.6, -0.75, 0.55));',
  '  float spec = pow(max(dot(n3, lightDir), 0.0), 12.0);',
  '  float rim = smoothstep(0.0, 0.9, curve);',
  '  float stroke = smoothstep(-2.5, -1.5, sdf) *',
  '    (1.0 - smoothstep(-1.0, 0.0, sdf));',
  '  float topBias =',
  '    0.5 + 0.5 * (-v_local.y / max(halfSize.y, 1.0));',
  '  c += (spec * rim * 1.4 +',
  '    stroke * (0.4 + 0.6 * topBias) * 0.55) * u_edgeLight;',
  '',
  '  c = mix(c, u_overlay.rgb, u_overlay.a);',
  '  gl_FragColor = vec4(applyCss(c), 1.0);',
  '}',
].join('\n');

const parseColor = (value, fallback) => {
  const match = String(value || '').match(
    /rgba?\(\s*(\d+)[,\s]+(\d+)[,\s]+(\d+)/,
  );
  if (!match) return fallback;
  return [
    Number(match[1]) / 255,
    Number(match[2]) / 255,
    Number(match[3]) / 255,
  ];
};

export const createSiteBackgroundGlassRenderer = ({ canvas, onFallback }) => {
  let gl = null;
  let destroyed = false;
  let failed = false;

  const fail = (reason) => {
    if (failed || destroyed) return;
    failed = true;
    onFallback?.(reason);
  };

  try {
    const attrs = {
      alpha: false,
      antialias: false,
      depth: false,
      stencil: false,
      preserveDrawingBuffer: false,
      powerPreference: 'high-performance',
    };
    gl =
      canvas.getContext('webgl', attrs) ||
      canvas.getContext('experimental-webgl', attrs);
  } catch {
    gl = null;
  }
  if (!gl) return null;

  const compile = (type, source) => {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      throw new Error(gl.getShaderInfoLog(shader) || 'shader compile failed');
    }
    return shader;
  };
  const link = (vs, fs, attrNames) => {
    const program = gl.createProgram();
    gl.attachShader(program, compile(gl.VERTEX_SHADER, vs));
    gl.attachShader(program, compile(gl.FRAGMENT_SHADER, fs));
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      throw new Error(gl.getProgramInfoLog(program) || 'link failed');
    }
    const wrapped = { program, attrs: {}, uniforms: {} };
    attrNames.forEach((name) => {
      wrapped.attrs[name] = gl.getAttribLocation(program, name);
    });
    const count = gl.getProgramParameter(program, gl.ACTIVE_UNIFORMS);
    for (let i = 0; i < count; i += 1) {
      const info = gl.getActiveUniform(program, i);
      const name = info.name.replace(/\[0\]$/, '');
      wrapped.uniforms[name] = gl.getUniformLocation(program, name);
    }
    return wrapped;
  };

  let progBg;
  let progDown;
  let progBlur;
  let progGlass;
  try {
    progBg = link(VS_QUAD, FS_BG, ['a_pos']);
    progDown = link(VS_QUAD, FS_DOWN, ['a_pos']);
    progBlur = link(VS_QUAD, FS_BLUR, ['a_pos']);
    progGlass = link(VS_GLASS, FS_GLASS, ['a_pos', 'a_local', 'a_shape']);
  } catch {
    return null;
  }

  const quadBuffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, quadBuffer);
  gl.bufferData(
    gl.ARRAY_BUFFER,
    new Float32Array([-1, -1, 3, -1, -1, 3]),
    gl.STATIC_DRAW,
  );
  const glassBuffer = gl.createBuffer();
  gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
  gl.disable(gl.DEPTH_TEST);
  gl.disable(gl.BLEND);

  const state = {
    image: null,
    imageTexture: null,
    fit: 'cover',
    overlayOpacity: 0.25,
    refraction: 0,
    clarity: 0.7,
    dispersion: 0.24,
    edgeLight: 0.35,
    cssWidth: 0,
    cssHeight: 0,
    fitVec: [1, 1, 0, 0],
    needBake: true,
    dirty: true,
    rafId: 0,
    elements: [],
    elementsStale: true,
    blurTexture: null,
    fbos: null,
    glassFloats: new Float32Array(0),
  };
  const radiusCache = new WeakMap();

  const makeFbo = (width, height) => {
    const texture = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.texImage2D(
      gl.TEXTURE_2D,
      0,
      gl.RGBA,
      width,
      height,
      0,
      gl.RGBA,
      gl.UNSIGNED_BYTE,
      null,
    );
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    const fbo = gl.createFramebuffer();
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(
      gl.FRAMEBUFFER,
      gl.COLOR_ATTACHMENT0,
      gl.TEXTURE_2D,
      texture,
      0,
    );
    const status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    if (status !== gl.FRAMEBUFFER_COMPLETE) return null;
    return { fbo, texture, width, height };
  };

  const releaseFbos = () => {
    if (!state.fbos) return;
    Object.values(state.fbos).forEach((entry) => {
      gl.deleteFramebuffer(entry.fbo);
      gl.deleteTexture(entry.texture);
    });
    state.fbos = null;
  };

  const drawQuad = (program) => {
    gl.bindBuffer(gl.ARRAY_BUFFER, quadBuffer);
    gl.enableVertexAttribArray(program.attrs.a_pos);
    gl.vertexAttribPointer(program.attrs.a_pos, 2, gl.FLOAT, false, 0, 0);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
  };

  const themeColors = () => {
    const dark = document.body.getAttribute('theme-mode') === 'dark';
    const layerBg = parseColor(
      getComputedStyle(canvas.parentElement || document.body).backgroundColor,
      dark ? [0.09, 0.09, 0.11] : [1, 1, 1],
    );
    return {
      dark,
      bgColor: layerBg,
      // 与 .site-background-overlay 一致：亮色白色遮罩，暗色黑色遮罩
      overlay: dark ? [0, 0, 0] : [1, 1, 1],
      saturate: 1.15,
      brightness: dark ? 1.02 : 1.04,
      contrast: dark ? 0.9 : 0.88,
      // 暗色下色散削弱，与 CSS 路径的 DARK_DISPERSION_RATIO 同理
      dispersionRatio: dark ? 0.3 : 1,
    };
  };

  const updateFit = () => {
    if (!state.image) return;
    const iw = state.image.naturalWidth || state.image.width;
    const ih = state.image.naturalHeight || state.image.height;
    const vw = state.cssWidth;
    const vh = state.cssHeight;
    if (!iw || !ih || !vw || !vh) return;
    let scale;
    if (state.fit === 'contain') {
      scale = Math.min(vw / iw, vh / ih);
    } else if (state.fit === 'fill') {
      state.fitVec = [1, 1, 0, 0];
      return;
    } else {
      scale = Math.max(vw / iw, vh / ih);
    }
    const uw = vw / (iw * scale);
    const uh = vh / (ih * scale);
    state.fitVec = [uw, uh, (1 - uw) / 2, (1 - uh) / 2];
  };

  const resize = () => {
    const width = canvas.clientWidth || window.innerWidth;
    const height = canvas.clientHeight || window.innerHeight;
    const ratio = Math.min(window.devicePixelRatio || 1, MAX_PIXEL_RATIO);
    state.cssWidth = width;
    state.cssHeight = height;
    canvas.width = Math.max(1, Math.round(width * ratio));
    canvas.height = Math.max(1, Math.round(height * ratio));
    releaseFbos();
    const half = makeFbo(
      Math.max(1, Math.round(canvas.width / 2)),
      Math.max(1, Math.round(canvas.height / 2)),
    );
    const quarter = makeFbo(
      Math.max(1, Math.round(canvas.width / 4)),
      Math.max(1, Math.round(canvas.height / 4)),
    );
    const eighthA = makeFbo(
      Math.max(1, Math.round(canvas.width / 8)),
      Math.max(1, Math.round(canvas.height / 8)),
    );
    const eighthB = makeFbo(
      Math.max(1, Math.round(canvas.width / 8)),
      Math.max(1, Math.round(canvas.height / 8)),
    );
    if (!half || !quarter || !eighthA || !eighthB) {
      fail('framebuffer');
      return;
    }
    state.fbos = { half, quarter, eighthA, eighthB };
    updateFit();
    state.needBake = true;
  };

  const bakeBlur = () => {
    if (!state.imageTexture || !state.fbos) return;
    const { half, quarter, eighthA, eighthB } = state.fbos;
    const colors = themeColors();

    gl.bindFramebuffer(gl.FRAMEBUFFER, half.fbo);
    gl.viewport(0, 0, half.width, half.height);
    gl.useProgram(progBg.program);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, state.imageTexture);
    gl.uniform1i(progBg.uniforms.u_tex, 0);
    gl.uniform4fv(progBg.uniforms.u_fit, state.fitVec);
    gl.uniform3fv(progBg.uniforms.u_bgColor, colors.bgColor);
    gl.uniform4f(progBg.uniforms.u_overlay, 0, 0, 0, 0);
    drawQuad(progBg);

    gl.useProgram(progDown.program);
    gl.uniform1i(progDown.uniforms.u_tex, 0);
    const down = (src, dst) => {
      gl.bindFramebuffer(gl.FRAMEBUFFER, dst.fbo);
      gl.viewport(0, 0, dst.width, dst.height);
      gl.bindTexture(gl.TEXTURE_2D, src.texture);
      gl.uniform2f(progDown.uniforms.u_texel, 1 / src.width, 1 / src.height);
      drawQuad(progDown);
    };
    down(half, quarter);
    down(quarter, eighthA);

    gl.useProgram(progBlur.program);
    gl.uniform1i(progBlur.uniforms.u_tex, 0);
    gl.viewport(0, 0, eighthA.width, eighthA.height);
    let src = eighthA;
    let dst = eighthB;
    for (let i = 0; i < 3; i += 1) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, dst.fbo);
      gl.bindTexture(gl.TEXTURE_2D, src.texture);
      gl.uniform2f(progBlur.uniforms.u_step, 1 / src.width, 0);
      drawQuad(progBlur);
      let tmp = src;
      src = dst;
      dst = tmp;
      gl.bindFramebuffer(gl.FRAMEBUFFER, dst.fbo);
      gl.bindTexture(gl.TEXTURE_2D, src.texture);
      gl.uniform2f(progBlur.uniforms.u_step, 0, 1 / src.height);
      drawQuad(progBlur);
      tmp = src;
      src = dst;
      dst = tmp;
    }
    state.blurTexture = src.texture;
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  };

  const refreshElements = () => {
    state.elements = Array.prototype.slice.call(
      document.querySelectorAll(GLASS_ELEMENT_SELECTORS),
      0,
      MAX_TRACKED_ELEMENTS,
    );
    state.elementsStale = false;
  };

  const radiusOf = (element) => {
    let radius = radiusCache.get(element);
    if (radius === undefined) {
      radius = Number.parseFloat(getComputedStyle(element).borderTopLeftRadius);
      if (!Number.isFinite(radius)) radius = 0;
      radiusCache.set(element, radius);
    }
    return radius;
  };

  const collectRects = () => {
    if (state.elementsStale) refreshElements();
    const vw = state.cssWidth;
    const vh = state.cssHeight;
    const rects = [];
    for (const element of state.elements) {
      if (!element.isConnected) {
        state.elementsStale = true;
        continue;
      }
      const rect = element.getBoundingClientRect();
      if (
        rect.width < 2 ||
        rect.height < 2 ||
        rect.bottom < 0 ||
        rect.top > vh ||
        rect.right < 0 ||
        rect.left > vw
      ) {
        continue;
      }
      rects.push({
        x: rect.left,
        y: rect.top,
        w: rect.width,
        h: rect.height,
        r: radiusOf(element),
      });
    }
    return rects;
  };

  const draw = () => {
    if (failed || destroyed || !state.imageTexture || !state.fbos) return;
    if (state.needBake) {
      bakeBlur();
      state.needBake = false;
    }
    const colors = themeColors();
    const overlay = [
      colors.overlay[0],
      colors.overlay[1],
      colors.overlay[2],
      state.overlayOpacity,
    ];

    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    gl.viewport(0, 0, canvas.width, canvas.height);

    gl.useProgram(progBg.program);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, state.imageTexture);
    gl.uniform1i(progBg.uniforms.u_tex, 0);
    gl.uniform4fv(progBg.uniforms.u_fit, state.fitVec);
    gl.uniform3fv(progBg.uniforms.u_bgColor, colors.bgColor);
    gl.uniform4fv(progBg.uniforms.u_overlay, overlay);
    drawQuad(progBg);

    const rects = collectRects();
    if (!rects.length || !state.blurTexture) return;

    const floatsNeeded = rects.length * 6 * 7;
    if (state.glassFloats.length < floatsNeeded) {
      state.glassFloats = new Float32Array(floatsNeeded * 2);
    }
    const floats = state.glassFloats;
    let offset = 0;
    const vw = state.cssWidth;
    const vh = state.cssHeight;
    for (const rect of rects) {
      const hx = rect.w / 2;
      const hy = rect.h / 2;
      const radius = Math.min(rect.r, hx, hy);
      const x0 = (rect.x / vw) * 2 - 1;
      const x1 = ((rect.x + rect.w) / vw) * 2 - 1;
      const y0 = 1 - (rect.y / vh) * 2;
      const y1 = 1 - ((rect.y + rect.h) / vh) * 2;
      const corners = [
        [x0, y0, -hx, -hy],
        [x1, y0, hx, -hy],
        [x1, y1, hx, hy],
        [x0, y0, -hx, -hy],
        [x1, y1, hx, hy],
        [x0, y1, -hx, hy],
      ];
      for (const corner of corners) {
        floats[offset] = corner[0];
        floats[offset + 1] = corner[1];
        floats[offset + 2] = corner[2];
        floats[offset + 3] = corner[3];
        floats[offset + 4] = hx;
        floats[offset + 5] = hy;
        floats[offset + 6] = radius;
        offset += 7;
      }
    }

    gl.useProgram(progGlass.program);
    gl.bindBuffer(gl.ARRAY_BUFFER, glassBuffer);
    gl.bufferData(gl.ARRAY_BUFFER, floats.subarray(0, offset), gl.DYNAMIC_DRAW);
    const stride = 7 * 4;
    gl.enableVertexAttribArray(progGlass.attrs.a_pos);
    gl.vertexAttribPointer(
      progGlass.attrs.a_pos,
      2,
      gl.FLOAT,
      false,
      stride,
      0,
    );
    gl.enableVertexAttribArray(progGlass.attrs.a_local);
    gl.vertexAttribPointer(
      progGlass.attrs.a_local,
      2,
      gl.FLOAT,
      false,
      stride,
      8,
    );
    gl.enableVertexAttribArray(progGlass.attrs.a_shape);
    gl.vertexAttribPointer(
      progGlass.attrs.a_shape,
      3,
      gl.FLOAT,
      false,
      stride,
      16,
    );

    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, state.blurTexture);
    gl.uniform1i(progGlass.uniforms.u_blur, 0);
    gl.activeTexture(gl.TEXTURE1);
    gl.bindTexture(gl.TEXTURE_2D, state.imageTexture);
    gl.uniform1i(progGlass.uniforms.u_bg, 1);
    gl.uniform4fv(progGlass.uniforms.u_fit, state.fitVec);
    gl.uniform3fv(progGlass.uniforms.u_bgColor, colors.bgColor);
    gl.uniform2f(progGlass.uniforms.u_res, vw, vh);
    const refractionRatio = state.refraction / 100;
    gl.uniform1f(
      progGlass.uniforms.u_bandFrac,
      (16 + refractionRatio * 10) / 100,
    );
    gl.uniform1f(progGlass.uniforms.u_scaleFrac, refractionRatio * 0.22);
    gl.uniform1f(
      progGlass.uniforms.u_dispersion,
      state.dispersion * colors.dispersionRatio,
    );
    gl.uniform1f(progGlass.uniforms.u_clarity, state.clarity);
    gl.uniform1f(progGlass.uniforms.u_edgeLight, state.edgeLight);
    gl.uniform4fv(progGlass.uniforms.u_overlay, overlay);
    gl.uniform3f(
      progGlass.uniforms.u_css,
      colors.saturate,
      colors.brightness,
      colors.contrast,
    );
    gl.drawArrays(gl.TRIANGLES, 0, rects.length * 6);
    gl.disableVertexAttribArray(progGlass.attrs.a_local);
    gl.disableVertexAttribArray(progGlass.attrs.a_shape);
    gl.activeTexture(gl.TEXTURE0);
  };

  const scheduleDraw = () => {
    if (state.rafId || failed || destroyed) return;
    state.rafId = window.requestAnimationFrame(() => {
      state.rafId = 0;
      if (!state.dirty) return;
      state.dirty = false;
      draw();
      // 滚动过程中每帧都会重新置脏；静止后这里自然停摆，GPU 归零
      if (state.dirty) scheduleDraw();
    });
  };

  const markDirty = () => {
    if (failed || destroyed) return;
    state.dirty = true;
    scheduleDraw();
  };

  const onScroll = () => markDirty();
  let resizeTimer = 0;
  const onResize = () => {
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => {
      resize();
      markDirty();
    }, 120);
  };
  const onContextLost = (event) => {
    event.preventDefault();
    fail('context-lost');
  };

  let mutationTimer = 0;
  const mutationObserver = new MutationObserver(() => {
    if (mutationTimer) return;
    mutationTimer = window.setTimeout(() => {
      mutationTimer = 0;
      state.elementsStale = true;
      markDirty();
    }, 200);
  });
  const themeObserver = new MutationObserver(() => {
    state.needBake = true;
    markDirty();
  });

  document.addEventListener('scroll', onScroll, {
    capture: true,
    passive: true,
  });
  window.addEventListener('resize', onResize);
  canvas.addEventListener('webglcontextlost', onContextLost, false);
  mutationObserver.observe(document.body, { childList: true, subtree: true });
  themeObserver.observe(document.body, {
    attributes: true,
    attributeFilter: ['theme-mode'],
  });

  resize();

  return {
    setImage(url) {
      if (failed || destroyed) return;
      const image = new Image();
      image.crossOrigin = 'anonymous';
      image.referrerPolicy = 'no-referrer';
      image.onload = () => {
        if (failed || destroyed) return;
        state.image = image;
        if (!state.imageTexture) state.imageTexture = gl.createTexture();
        gl.activeTexture(gl.TEXTURE0);
        gl.bindTexture(gl.TEXTURE_2D, state.imageTexture);
        try {
          gl.texImage2D(
            gl.TEXTURE_2D,
            0,
            gl.RGBA,
            gl.RGBA,
            gl.UNSIGNED_BYTE,
            image,
          );
        } catch {
          // 图源没有放开跨域时纹理不可用，退回 CSS 路径
          fail('cors');
          return;
        }
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
        updateFit();
        state.needBake = true;
        markDirty();
      };
      // CSS 背景不要求跨域，但 WebGL 纹理要求；加载失败即退回 CSS 路径
      image.onerror = () => fail('cors');
      image.src = url;
    },
    setOptions(options) {
      if (failed || destroyed) return;
      if (options.fit && options.fit !== state.fit) {
        state.fit = options.fit;
        updateFit();
        state.needBake = true;
      }
      if (Number.isFinite(options.overlayOpacity)) {
        state.overlayOpacity = options.overlayOpacity;
      }
      if (Number.isFinite(options.refraction)) {
        state.refraction = options.refraction;
      }
      if (Number.isFinite(options.clarity)) {
        state.clarity = options.clarity;
      }
      if (Number.isFinite(options.dispersion)) {
        state.dispersion = options.dispersion;
      }
      if (Number.isFinite(options.edgeLight)) {
        state.edgeLight = options.edgeLight;
      }
      markDirty();
    },
    destroy() {
      destroyed = true;
      document.removeEventListener('scroll', onScroll, { capture: true });
      window.removeEventListener('resize', onResize);
      canvas.removeEventListener('webglcontextlost', onContextLost, false);
      mutationObserver.disconnect();
      themeObserver.disconnect();
      window.clearTimeout(resizeTimer);
      window.clearTimeout(mutationTimer);
      if (state.rafId) window.cancelAnimationFrame(state.rafId);
      releaseFbos();
      if (state.imageTexture) gl.deleteTexture(state.imageTexture);
    },
  };
};
