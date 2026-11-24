package design

// families are the typefaces engine2 carries, each a variable font: one file per subset draws every weight
// of its range. Copied from the first engine's jit/fonts (05/2026 Google Fonts CSS, OFL and Apache: families/FONTS_LICENSE),
// keeping one of each set of identical files Google gives a variable family per weight.
var families = []family{
	{
		slug: "outfit", name: "Outfit",
		faces: []face{
			{lightest: 100, heaviest: 900, subset: "latin-ext", file: "families/outfit/100-900-latin-ext.woff2", unicode: "U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, U+0304, U+0308, U+0329, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C0, U+2113, U+2C60-2C7F, U+A720-A7FF"},
			{lightest: 100, heaviest: 900, subset: "latin", file: "families/outfit/100-900-latin.woff2", unicode: "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD"},
		},
	},
	{
		slug: "fira-code", name: "Fira Code",
		faces: []face{
			{lightest: 300, heaviest: 700, subset: "latin-ext", file: "families/fira-code/300-700-latin-ext.woff2", unicode: "U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, U+0304, U+0308, U+0329, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C0, U+2113, U+2C60-2C7F, U+A720-A7FF"},
			{lightest: 300, heaviest: 700, subset: "latin", file: "families/fira-code/300-700-latin.woff2", unicode: "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD"},
		},
	},
	{
		slug: "google-sans-flex", name: "Google Sans Flex",
		faces: []face{
			{lightest: 1, heaviest: 1000, subset: "vietnamese", file: "families/google-sans-flex/1-1000-vietnamese.woff2", unicode: "U+0102-0103, U+0110-0111, U+0128-0129, U+0168-0169, U+01A0-01A1, U+01AF-01B0, U+0300-0301, U+0303-0304, U+0308-0309, U+0323, U+0329, U+1EA0-1EF9, U+20AB"},
			{lightest: 1, heaviest: 1000, subset: "latin-ext", file: "families/google-sans-flex/1-1000-latin-ext.woff2", unicode: "U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, U+0304, U+0308, U+0329, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C4, U+2113, U+2C60-2C7F, U+A720-A7FF"},
			{lightest: 1, heaviest: 1000, subset: "latin", file: "families/google-sans-flex/1-1000-latin.woff2", unicode: "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD"},
		},
	},
	{
		slug: "google-sans-code", name: "Google Sans Code",
		faces: []face{
			{lightest: 300, heaviest: 800, subset: "vietnamese", file: "families/google-sans-code/300-800-vietnamese.woff2", unicode: "U+0102-0103, U+0110-0111, U+0128-0129, U+0168-0169, U+01A0-01A1, U+01AF-01B0, U+0300-0301, U+0303-0304, U+0308-0309, U+0323, U+0329, U+1EA0-1EF9, U+20AB"},
			{lightest: 300, heaviest: 800, subset: "latin-ext", file: "families/google-sans-code/300-800-latin-ext.woff2", unicode: "U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, U+0304, U+0308, U+0329, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C4, U+2113, U+2C60-2C7F, U+A720-A7FF"},
			{lightest: 300, heaviest: 800, subset: "latin", file: "families/google-sans-code/300-800-latin.woff2", unicode: "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD"},
		},
	},
	{
		slug: "jetbrains-mono", name: "JetBrains Mono",
		faces: []face{
			{lightest: 100, heaviest: 800, subset: "vietnamese", file: "families/jetbrains-mono/100-800-vietnamese.woff2", unicode: "U+0102-0103, U+0110-0111, U+0128-0129, U+0168-0169, U+01A0-01A1, U+01AF-01B0, U+0300-0301, U+0303-0304, U+0308-0309, U+0323, U+0329, U+1EA0-1EF9, U+20AB"},
			{lightest: 100, heaviest: 800, subset: "latin-ext", file: "families/jetbrains-mono/100-800-latin-ext.woff2", unicode: "U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, U+0304, U+0308, U+0329, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C4, U+2113, U+2C60-2C7F, U+A720-A7FF"},
			{lightest: 100, heaviest: 800, subset: "latin", file: "families/jetbrains-mono/100-800-latin.woff2", unicode: "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD"},
		},
	},
}
