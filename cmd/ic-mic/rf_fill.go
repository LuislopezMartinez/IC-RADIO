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
    vec3 cool = vec3(0.30, 0.44, 0.54);
    vec3 teal = vec3(0.24, 0.71, 0.81);
    vec3 warm = vec3(0.98, 0.65, 0.26);
    vec3 hot = vec3(0.96, 0.36, 0.17);
    vec3 tint = mix(cool, teal, smoothstep(0.0, 9.0, contrastDb));
    tint = mix(tint, warm, smoothstep(7.0, 20.0, contrastDb));
    tint = mix(tint, hot, smoothstep(22.0, 37.0, contrastDb));
    finalColor = vec4(tint, 0.64);
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
		rl.DrawTriangleStrip(points, rl.NewColor(124, 157, 167, 135))
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
