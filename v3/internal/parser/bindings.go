package parser

import (
	"sort"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

const header = `// @ts-check
// Cynhyrchwyd y ffeil hon yn awtomatig. PEIDIWCH Â MODIWL
// This file is automatically generated. DO NOT EDIT

`

const helperTemplate = `function {{structName}}(method) {
    return {
        packageName: "{{packageName}}",
        serviceName: "{{structName}}",
        methodName: method,
        args: Array.prototype.slice.call(arguments, 1),
    };
}
`

func GenerateHelper(packageName, structName string) string {
	result := strings.ReplaceAll(helperTemplate, "{{packageName}}", packageName)
	result = strings.ReplaceAll(result, "{{structName}}", structName)
	return result
}

const bindingTemplate = `
/**
 * {{structName}}.{{methodName}}
 * Comments
 * @param name {string}
 * @returns {Promise<string>}
 **/
function {{methodName}}({{inputs}}) {
    return wails.Call({{structName}}("{{methodName}}"{{args}}));
}
`

func sanitiseJSVarName(name string) string {
	// if the name is a reserved word, prefix with an
	// underscore
	if strings.Contains("break,case,catch,class,const,continue,debugger,default,delete,do,else,enum,export,extends,false,finally,for,function,if,implements,import,in,instanceof,interface,let,new,null,package,private,protected,public,return,static,super,switch,this,throw,true,try,typeof,var,void,while,with,yield", name) {
		return "_" + name
	}
	return name
}

func GenerateBinding(structName string, method *BoundMethod) (string, []string) {
	var models []string
	result := strings.ReplaceAll(bindingTemplate, "{{structName}}", structName)
	result = strings.ReplaceAll(result, "{{methodName}}", method.Name)
	comments := strings.TrimSpace(method.DocComment)
	result = strings.ReplaceAll(result, "Comments", comments)
	var params string
	for _, input := range method.Inputs {
		pkgName := getPackageName(input)
		if pkgName != "" {
			models = append(models, pkgName)
		}
		params += " * @param " + sanitiseJSVarName(input.Name) + " {" + input.JSType() + "}\n"
	}
	params = strings.TrimSuffix(params, "\n")
	if len(params) == 0 {
		params = " *"
	}
	////params += "\n"
	result = strings.ReplaceAll(result, " * @param name {string}", params)
	var inputs string
	for _, input := range method.Inputs {
		pkgName := getPackageName(input)
		if pkgName != "" {
			models = append(models, pkgName)
		}
		inputs += sanitiseJSVarName(input.Name) + ", "
	}
	inputs = strings.TrimSuffix(inputs, ", ")
	args := inputs
	if len(args) > 0 {
		args = ", " + args
	}
	result = strings.ReplaceAll(result, "{{inputs}}", inputs)
	result = strings.ReplaceAll(result, "{{args}}", args)

	// outputs
	var returns string
	if len(method.Outputs) == 0 {
		returns = " * @returns {Promise<void>}"
	} else {
		returns = " * @returns {Promise<"
		for _, output := range method.Outputs {
			pkgName := getPackageName(output)
			if pkgName != "" {
				models = append(models, pkgName)
			}
			jsType := output.JSType()
			if jsType == "error" {
				jsType = "void"
			}
			returns += jsType + ", "
		}
		returns = strings.TrimSuffix(returns, ", ")
		returns += ">}"
	}
	result = strings.ReplaceAll(result, " * @returns {Promise<string>}", returns)

	return result, lo.Uniq(models)
}

func getPackageName(input *Parameter) string {
	if !input.Type.IsStruct {
		return ""
	}
	result := input.Type.Package
	if result == "" {
		result = "main"
	}
	return result
}

func normalisePackageNames(packageNames []string) map[string]string {
	// We iterate over the package names and determine if any of them
	// have a forward slash. If this is the case, we assume that the
	// package name is the last element of the path. If this has already
	// been found, then we need to add a digit to the end of the package
	// name to make it unique. We return a map of the original package
	// name to the new package name.
	var result = make(map[string]string)
	var packagesConverted = make(map[string]struct{})
	var count = 1
	for _, packageName := range packageNames {
		var originalPackageName = packageName
		if strings.Contains(packageName, "/") {
			parts := strings.Split(packageName, "/")
			packageName = parts[len(parts)-1]
		}
		if _, ok := packagesConverted[packageName]; ok {
			// We've already seen this package name. Add a digit
			// to the end of the package name to make it unique
			count += 1
			packageName += strconv.Itoa(count)

		}
		packagesConverted[packageName] = struct{}{}
		result[originalPackageName] = packageName
	}

	return result
}

func GenerateBindings(bindings map[string]map[string][]*BoundMethod) map[string]string {

	var result = make(map[string]string)

	var normalisedPackageNames = normalisePackageNames(lo.Keys(bindings))
	// sort the bindings keys
	packageNames := lo.Keys(bindings)
	sort.Strings(packageNames)
	for _, packageName := range packageNames {
		var allModels []string

		packageBindings := bindings[packageName]
		structNames := lo.Keys(packageBindings)
		sort.Strings(structNames)
		for _, structName := range structNames {
			result[normalisedPackageNames[packageName]] += GenerateHelper(normalisedPackageNames[packageName], structName)
			methods := packageBindings[structName]
			sort.Slice(methods, func(i, j int) bool {
				return methods[i].Name < methods[j].Name
			})
			for _, method := range methods {
				thisBinding, models := GenerateBinding(structName, method)
				result[normalisedPackageNames[packageName]] += thisBinding
				allModels = append(allModels, models...)
			}
		}

		result[normalisedPackageNames[packageName]] += `
window.go = window.go || {};
`
		// Iterate over the sorted struct keys
		result[normalisedPackageNames[packageName]] += "window.go." + normalisedPackageNames[packageName] + " = {\n"
		for _, structName := range structNames {
			result[normalisedPackageNames[packageName]] += "    " + structName + ": {\n"
			methods := packageBindings[structName]
			sort.Slice(methods, func(i, j int) bool {
				return methods[i].Name < methods[j].Name
			})
			for _, method := range methods {
				result[normalisedPackageNames[packageName]] += "        " + method.Name + ",\n"
			}
			result[normalisedPackageNames[packageName]] += "    },\n"
		}
		result[normalisedPackageNames[packageName]] += "};\n"

		// add imports
		if len(allModels) > 0 {
			allModels := lo.Uniq(allModels)
			var models []string
			for _, model := range allModels {
				models = append(models, normalisedPackageNames[model])
			}
			sort.Strings(models)
			result[normalisedPackageNames[packageName]] += "\n"
			imports := "import {" + strings.Join(models, ", ") + "} from './models';\n"
			result[normalisedPackageNames[packageName]] = imports + "\n" + result[normalisedPackageNames[packageName]]
		}

		result[normalisedPackageNames[packageName]] = header + result[normalisedPackageNames[packageName]]
	}

	return result
}
