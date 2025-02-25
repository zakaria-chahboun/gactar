# amod Config

## gactar Config

Top-level configuration in the `gactar` section.

Example:

```
gactar {
    log_level: 'detail'
    trace_activations: true
}
```

| Config            | Type                                       | Description                                                                              |
| ----------------- | ------------------------------------------ | ---------------------------------------------------------------------------------------- |
| log_level         | string (one of 'min', 'info', or 'detail') | how verbose our logging should be                                                        |
| trace_activations | boolean                                    | output detailed info about activations                                                   |
| random_seed       | positive integer                           | sets the seed to use for generating pseudo-random numbers (allows for reproducible runs) |

## Module Config

gactar supports a handful of modules and configuration options. The following outlines which options are available in the `modules` section.

Example:

```
modules {
    memory {
        latency_factor: 0.63
        max_spread_strength: 1.6
    }

    goal {
        spreading_activation: 1.0
    }

    extra_buffers {
        foo {}
        bar {}
    }
}
```

### Declarative Memory

This is the standard ACT-R declarative memory module.

Module Name: **memory**

Buffer Name: **retrieval**

| Config              | Type    | Description                                                                           | Mapping                                                                                                                     |
| ------------------- | ------- | ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| decay               | decimal | sets the decay for the base-level learning calculation                                | ccm (DMBaseLevel submodule 'decay'): 0.5<br>pyactr (decay) : 0.5<br>vanilla (:bll): nil (recommend 0.5 if used)             |
| finst_size          | integer | how many chunks are retained in memory                                                | ccm (finst_size): 4<br>pyactr (DecMemBuffer.finst): 0<br>vanilla (:declarative-num-finsts): 4                               |
| finst_time          | decimal | how long the finst lasts in memory                                                    | ccm (finst_time): 3.0<br>pyactr: (unsupported? Always ∞ I guess?)<br>vanilla (:declarative-finst-span): 3.0                 |
| instantaneous_noise | decimal | turns on noise calculation & sets instantaneous noise                                 | ccm (DMNoise submodule 'noise')<br>pyactr (instantaneous_noise)<br>vanilla (:ans)                                           |
| latency_exponent    | decimal | latency exponent (f)                                                                  | ccm: (unsupported? Based on the code, it seems to be fixed at 1.0.)<br>pyactr (latency_exponent): 1.0<br>vanilla (:le): 1.0 |
| latency_factor      | decimal | latency factor (F)                                                                    | ccm (latency): 0.05<br>pyactr (latency_factor): 0.1<br>vanilla (:lf): 1.0                                                   |
| max_spread_strength | decimal | turns on the spreading activation calculation & sets the maximum associative strength | ccm (DMSpreading submodule)<br>pyactr (strength_of_association)<br>vanilla (:mas)                                           |
| mismatch_penalty    | decimal | turns on partial matching and sets the penalty in the activation equation to this     | ccm (Partial class)<br>pyactr (partial_matching & mismatch_penalty)<br>vanilla (:mp)                                        |
| retrieval_threshold | decimal | retrieval threshold (τ)                                                               | ccm (threshold): 0.0<br>pyactr (retrieval_threshold): 0.0<br>vanilla (:rt): 0.0                                             |

### Goal

This is the standard ACT-R goal module.

Module Name: **goal**

Buffer Name: **goal**

| Config               | Type    | Description                                                         | Mapping                                                                                          |
| -------------------- | ------- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| spreading_activation | decimal | see "Spreading Activation" in "ACT-R 7.26 Reference Manual" pg. 290 | ccm (DMSpreading.weight): 1.0<br>pyactr (buffer_spreading_activation): 1.0<br>vanilla (:ga): 1.0 |

### Imaginal

This is the standard ACT-R imaginal module.

Module Name: **imaginal**

Buffer Name: **imaginal**

| Config | Type    | Description                                                     | Mapping                                                                                       |
| ------ | ------- | --------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| delay  | decimal | how long it takes a request to the buffer to complete (seconds) | ccm (ImaginalModule.delay): 0.2<br>pyactr (Goal.delay): 0.2<br>vanilla (:imaginal-delay): 0.2 |

### Procedural

This is the standard ACT-R procedural module.

Module Name: **procedural**

Buffer Name: _none_

| Config              | Type    | Description                                       | Mapping                                                                           |
| ------------------- | ------- | ------------------------------------------------- | --------------------------------------------------------------------------------- |
| default_action_time | decimal | time that it takes to fire a production (seconds) | ccm (production_time): 0.05<br>pyactr (rule_firing): 0.05<br>vanilla (:dat): 0.05 |

### Extra Buffers

This is a gactar-specific module used to add new buffers to the model. According to ACT-R, buffers should only be added through modules, however some implementations allow declaring them wherever you want.

Module Name: **extra_buffers**

Buffer Names: _specified in configuration_

| Config           | Description                                                                                            |
| ---------------- | ------------------------------------------------------------------------------------------------------ |
| _buffer name_ {} | the name of the new buffer (with "{}" to allow the possibility of buffer config options in the future) |
