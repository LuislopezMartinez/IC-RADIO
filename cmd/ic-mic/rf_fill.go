package main

import rl "github.com/gen2brain/raylib-go/raylib"

const rfFillVertex = `#version 330
in vec3 vertexPosition;
uniform mat4 mvp;
out float plotY;
void main() {
    plotY = vertexPosition.y;
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}`

const rfFillFragment = `#version 330
in float plotY;
uniform vec4 plot; // bottom, height, vertical zoom, noise floor
out vec4 finalColor;
void main() {
    float shown = 160.0 * (plot.x - plotY) / plot.y;
    float level = plot.w + (shown - plot.w) / plot.z;
    float contrastDb = max(0.0, (level - plot.w) * 0.5);
    float elevation = clamp(shown / 160.0, 0.0, 1.0);
    vec3 lowerTint = vec3(0.18, 0.31, 0.28);
    vec3 traceTint = vec3(0.54, 0.69, 0.63);
    float rise = max(smoothstep(0.0, 0.58, elevation),
                     smoothstep(0.0, 30.0, contrastDb));
    vec3 tint = mix(lowerTint, traceTint, rise);
    finalColor = vec4(tint, mix(0.24, 0.64, rise));
}`

type rfFillGradient struct {
	shader      rl.Shader
	location    int32
	initialized bool
}

func (g *rfFillGradient) draw(points []rl.Vector2, graph rl.Rectangle, zoom rfVerticalZoom) {
	if !g.initialized {
		g.initialized = true
		g.shader = rl.LoadShaderFromMemory(rfFillVertex, rfFillFragment)
		g.location = rl.GetShaderLocation(g.shader, "plot")
	}
	if g.location < 0 {
		rl.DrawTriangleStrip(points, rl.NewColor(91, 133, 117, 88))
		return
	}
	rl.SetShaderValue(g.shader, g.location, []float32{graph.Y + graph.Height, graph.Height, zoom.factor(), zoom.floor}, rl.ShaderUniformVec4)
	rl.BeginShaderMode(g.shader)
	rl.DrawTriangleStrip(points, rl.White)
	rl.EndShaderMode()
}

func (g *rfFillGradient) unload() {
	if g.initialized {
		rl.UnloadShader(g.shader)
	}
}
